package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

type MysqlOutboxRepository struct {
	DB *sqlx.DB
}

func NewMysqlOutboxRepository(db *sqlx.DB) OutboxRepository {
	return &MysqlOutboxRepository{DB: db}
}

func (r *MysqlOutboxRepository) InsertEvent(ctx context.Context, event *Event) error {
	query := `INSERT INTO outbox_events
		(event_id, aggregate_type, aggregate_id, event_type, kafka_topic, kafka_key, payload, status, retry_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'PENDING', 0, ?)`

	tx, ok := infrastructure.ExtractTx(ctx)
	if ok {
		_, err := tx.ExecContext(ctx, query,
			event.EventID, event.AggregateType, event.AggregateID,
			event.EventType, event.KafkaTopic, event.KafkaKey,
			event.Payload, time.Now())
		if err != nil {
			return fmt.Errorf("insert outbox event: %w", err)
		}
		return nil
	}

	_, err := r.DB.ExecContext(ctx, query,
		event.EventID, event.AggregateType, event.AggregateID,
		event.EventType, event.KafkaTopic, event.KafkaKey,
		event.Payload, time.Now())
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (r *MysqlOutboxRepository) FetchPending(ctx context.Context, limit int) ([]*Event, error) {
	// Exponential backoff at DB level (02_Code.md 3.2):
	// retry_count=0 → immediate, retry_count=1 → 2s, retry_count=2 → 4s, retry_count=3 → 8s
	query := `SELECT id, event_id, aggregate_type, aggregate_id, event_type, kafka_topic, kafka_key, payload, status, retry_count, created_at, sent_at
		FROM outbox_events
		WHERE status = 'PENDING'
		  AND (retry_count = 0 OR created_at < NOW() - INTERVAL POW(2, retry_count) SECOND)
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED`

	var events []*Event
	if err := r.DB.SelectContext(ctx, &events, query, limit); err != nil {
		return nil, fmt.Errorf("fetch pending outbox events: %w", err)
	}
	return events, nil
}

func (r *MysqlOutboxRepository) MarkSent(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	query, args, err := sqlx.In(`UPDATE outbox_events SET status = 'SENT', sent_at = ? WHERE id IN (?)`, time.Now(), ids)
	if err != nil {
		return fmt.Errorf("build mark sent query: %w", err)
	}
	query = r.DB.Rebind(query)
	_, err = r.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark sent outbox events: %w", err)
	}
	return nil
}

func (r *MysqlOutboxRepository) MarkFailed(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE outbox_events SET status = 'FAILED' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark failed outbox event: %w", err)
	}
	return nil
}

func (r *MysqlOutboxRepository) IncrementRetry(ctx context.Context, id int64) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE outbox_events SET retry_count = retry_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("increment retry count: %w", err)
	}
	return nil
}

func (r *MysqlOutboxRepository) CountStuckEvents(ctx context.Context) (int, error) {
	var count int
	err := r.DB.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM outbox_events WHERE status = 'PENDING' AND created_at < NOW() - INTERVAL 5 MINUTE`)
	if err != nil {
		return 0, fmt.Errorf("count stuck events: %w", err)
	}
	return count, nil
}

// MysqlIdempotencyRepository implements IdempotencyRepository.
type MysqlIdempotencyRepository struct {
	DB *sqlx.DB
}

func NewMysqlIdempotencyRepository(db *sqlx.DB) IdempotencyRepository {
	return &MysqlIdempotencyRepository{DB: db}
}

func (r *MysqlIdempotencyRepository) Check(ctx context.Context, key string) ([]byte, bool, error) {
	var responseBody []byte
	err := r.DB.GetContext(ctx, &responseBody,
		`SELECT response_body FROM idempotency_keys WHERE idempotency_key = ? AND expires_at > NOW()`, key)
	if err != nil {
		return nil, false, nil // not found
	}
	return responseBody, true, nil
}

func (r *MysqlIdempotencyRepository) Save(ctx context.Context, key string, responseBody []byte) error {
	tx, ok := infrastructure.ExtractTx(ctx)
	if ok {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO idempotency_keys (idempotency_key, response_body, expires_at) VALUES (?, ?, DATE_ADD(NOW(), INTERVAL 24 HOUR))`,
			key, responseBody)
		if err != nil {
			return fmt.Errorf("save idempotency key: %w", err)
		}
		return nil
	}
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO idempotency_keys (idempotency_key, response_body, expires_at) VALUES (?, ?, DATE_ADD(NOW(), INTERVAL 24 HOUR))`,
		key, responseBody)
	if err != nil {
		return fmt.Errorf("save idempotency key: %w", err)
	}
	return nil
}
