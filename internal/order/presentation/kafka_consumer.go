package presentation

import (
	"context"
	"log/slog"
	"sync"

	"github.com/IBM/sarama"
	"github.com/jmoiron/sqlx"

	domain "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type EventHandler func(ctx context.Context, eventID string, payload []byte) error

type IdempotentConsumer struct {
	DB            *sqlx.DB
	Handler       EventHandler
	ConsumerGroup string
	DLQTopic      string
	DLQProducer   domain.EventProducer
	MaxRetries    int
	Logger        *slog.Logger

	retryMu     sync.Mutex
	retryCounts map[string]int
}

func NewIdempotentConsumer(
	db *sqlx.DB,
	handler EventHandler,
	consumerGroup string,
	dlqTopic string,
	dlqProducer domain.EventProducer,
	maxRetries int,
	logger *slog.Logger,
) *IdempotentConsumer {
	return &IdempotentConsumer{
		DB:            db,
		Handler:       handler,
		ConsumerGroup: consumerGroup,
		DLQTopic:      dlqTopic,
		DLQProducer:   dlqProducer,
		MaxRetries:    maxRetries,
		Logger:        logger,
		retryCounts:   make(map[string]int),
	}
}

// Setup implements sarama.ConsumerGroupHandler.
func (c *IdempotentConsumer) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup implements sarama.ConsumerGroupHandler.
func (c *IdempotentConsumer) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim processes messages with idempotency guarantee.
// 02_Code.md 3.3: processed_events 조회→비즈니스 로직→삽입을 단일 트랜잭션으로.
// 04_Fix.md EDA-1: DLQ handling via EventProducer facade (not direct sarama).
func (c *IdempotentConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		eventID := extractEventID(msg)
		if eventID == "" {
			c.Logger.Warn("message missing event_id header, skipping",
				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset)
			session.MarkMessage(msg, "")
			continue
		}

		if err := c.processWithIdempotency(session.Context(), eventID, msg.Value); err != nil {
			c.Logger.Error("failed to process message",
				"event_id", eventID,
				"topic", msg.Topic,
				"error", err)

			c.retryMu.Lock()
			c.retryCounts[eventID]++
			count := c.retryCounts[eventID]
			c.retryMu.Unlock()

			if count >= c.MaxRetries {
				c.publishToDLQ(session.Context(), msg, eventID, err)
				session.MarkMessage(msg, "")

				c.retryMu.Lock()
				delete(c.retryCounts, eventID)
				c.retryMu.Unlock()
			}
			continue
		}

		c.retryMu.Lock()
		delete(c.retryCounts, eventID)
		c.retryMu.Unlock()

		session.MarkMessage(msg, "")
	}
	return nil
}

// publishToDLQ sends failed messages to the DLQ topic via EventProducer facade.
func (c *IdempotentConsumer) publishToDLQ(ctx context.Context, msg *sarama.ConsumerMessage, eventID string, processErr error) {
	if c.DLQProducer == nil || c.DLQTopic == "" {
		c.Logger.Error("DLQ not configured, event dropped",
			"event_id", eventID, "error", processErr)
		return
	}

	headers := map[string]string{
		"event_id":       eventID,
		"dlq_reason":     processErr.Error(),
		"original_topic": msg.Topic,
	}

	err := c.DLQProducer.SendMessage(ctx, c.DLQTopic, string(msg.Key), msg.Value, headers)
	if err != nil {
		c.Logger.Error("failed to publish to DLQ",
			"event_id", eventID, "dlq_topic", c.DLQTopic, "error", err)
	} else {
		c.Logger.Warn("event sent to DLQ",
			"event_id", eventID, "dlq_topic", c.DLQTopic, "reason", processErr.Error())
	}
}

func (c *IdempotentConsumer) processWithIdempotency(ctx context.Context, eventID string, payload []byte) error {
	tx, err := c.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	// 1. Check if already processed
	var count int
	err = tx.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM processed_events WHERE event_id = ? AND consumer_group = ?`,
		eventID, c.ConsumerGroup)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if count > 0 {
		_ = tx.Rollback()
		c.Logger.Debug("duplicate event skipped", "event_id", eventID)
		return nil
	}

	// 2. Process the event
	if err := c.Handler(ctx, eventID, payload); err != nil {
		_ = tx.Rollback()
		return err
	}

	// 3. Mark as processed
	_, err = tx.ExecContext(ctx,
		`INSERT INTO processed_events (event_id, consumer_group) VALUES (?, ?)`,
		eventID, c.ConsumerGroup)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func extractEventID(msg *sarama.ConsumerMessage) string {
	for _, h := range msg.Headers {
		if string(h.Key) == "event_id" {
			return string(h.Value)
		}
	}
	return ""
}
