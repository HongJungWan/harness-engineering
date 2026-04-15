package outbox

import (
	"context"
	"log/slog"
	"math"
	"time"

	domain "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type RelayConfig struct {
	PollInterval time.Duration
	BatchSize    int
	MaxRetries   int
	BackoffBase  time.Duration
}

type Relay struct {
	repo     OutboxRepository
	producer domain.EventProducer
	config   RelayConfig
	logger   *slog.Logger
}

func NewRelay(repo OutboxRepository, producer domain.EventProducer, cfg RelayConfig, logger *slog.Logger) *Relay {
	return &Relay{
		repo:     repo,
		producer: producer,
		config:   cfg,
		logger:   logger,
	}
}

// Start begins the polling loop. Blocks until ctx is cancelled.
// Also runs a stuck event detector every minute (04_Fix.md RELAY-5).
func (r *Relay) Start(ctx context.Context) {
	r.logger.Info("outbox relay started",
		"poll_interval", r.config.PollInterval,
		"batch_size", r.config.BatchSize)

	pollTicker := time.NewTicker(r.config.PollInterval)
	defer pollTicker.Stop()

	stuckTicker := time.NewTicker(1 * time.Minute)
	defer stuckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay stopped")
			return
		case <-pollTicker.C:
			if err := r.pollAndPublish(ctx); err != nil {
				r.logger.Error("relay poll error", "error", err)
			}
		case <-stuckTicker.C:
			r.detectStuckEvents(ctx)
		}
	}
}

func (r *Relay) detectStuckEvents(ctx context.Context) {
	count, err := r.repo.CountStuckEvents(ctx)
	if err != nil {
		r.logger.Error("stuck event detection failed", "error", err)
		return
	}
	if count > 0 {
		r.logger.Warn("stuck events detected: PENDING for over 5 minutes", "count", count)
	}
}

func (r *Relay) pollAndPublish(ctx context.Context) error {
	// FetchPending uses FOR UPDATE SKIP LOCKED (see mysql_outbox_repo.go)
	events, err := r.repo.FetchPending(ctx, r.config.BatchSize)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	var sentIDs []int64

	for _, evt := range events {
		headers := map[string]string{
			"event_id":   evt.EventID,
			"event_type": evt.EventType,
		}

		// Produce via EventProducer facade (acks=all configured at creation)
		err := r.producer.SendMessage(ctx, evt.KafkaTopic, evt.KafkaKey, evt.Payload, headers)
		if err != nil {
			r.logger.Error("kafka send failed",
				"event_id", evt.EventID,
				"topic", evt.KafkaTopic,
				"retry_count", evt.RetryCount,
				"error", err)

			// Exponential backoff: increment retry, mark failed if max exceeded
			_ = r.repo.IncrementRetry(ctx, evt.ID)
			if evt.RetryCount+1 >= r.config.MaxRetries {
				r.logger.Error("event max retries exceeded, marking FAILED",
					"event_id", evt.EventID,
					"retry_count", evt.RetryCount+1)
				_ = r.repo.MarkFailed(ctx, evt.ID)
			}
			continue
		}

		sentIDs = append(sentIDs, evt.ID)
	}

	// Mark successfully sent events as SENT (only after Kafka ack confirmed)
	if len(sentIDs) > 0 {
		if err := r.repo.MarkSent(ctx, sentIDs); err != nil {
			r.logger.Error("mark sent failed", "error", err, "count", len(sentIDs))
			return err
		}
		r.logger.Info("relay published events", "count", len(sentIDs))
	}

	return nil
}

// BackoffDuration calculates exponential backoff: base * 2^retryCount
func BackoffDuration(base time.Duration, retryCount int) time.Duration {
	return base * time.Duration(math.Pow(2, float64(retryCount)))
}
