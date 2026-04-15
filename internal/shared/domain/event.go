package domain

import "time"

type DomainEvent interface {
	EventID() string
	EventType() string
	AggregateType() string
	AggregateID() string
	OccurredAt() time.Time
	Payload() ([]byte, error)
	KafkaTopic() string
	KafkaKey() string
}
