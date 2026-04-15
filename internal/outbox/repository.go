package outbox

import (
	"context"
	"time"
)

type Event struct {
	ID            int64
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	KafkaTopic    string
	KafkaKey      string
	Payload       []byte
	Status        string
	RetryCount    int
	CreatedAt     time.Time
	SentAt        *time.Time
}

type OutboxRepository interface {
	InsertEvent(ctx context.Context, event *Event) error
	FetchPending(ctx context.Context, limit int) ([]*Event, error)
	MarkSent(ctx context.Context, ids []int64) error
	MarkFailed(ctx context.Context, id int64) error
	IncrementRetry(ctx context.Context, id int64) error
	CountStuckEvents(ctx context.Context) (int, error)
}

type IdempotencyRepository interface {
	Check(ctx context.Context, key string) (responseBody []byte, found bool, err error)
	Save(ctx context.Context, key string, responseBody []byte) error
}
