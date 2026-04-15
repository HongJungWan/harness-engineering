package domain

import "context"

// EventProducer abstracts message broker interactions.
// Facade pattern: relay.go and kafka_consumer.go depend on this interface
// instead of directly coupling to sarama (or any specific broker library).
type EventProducer interface {
	SendMessage(ctx context.Context, topic, key string, payload []byte, headers map[string]string) error
}
