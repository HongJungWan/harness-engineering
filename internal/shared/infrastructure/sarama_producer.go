package infrastructure

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"

	domain "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

// SaramaProducer wraps sarama.SyncProducer behind the domain.EventProducer facade.
// This is the only file that imports sarama for producing messages.
type SaramaProducer struct {
	producer sarama.SyncProducer
}

func NewSaramaProducer(producer sarama.SyncProducer) domain.EventProducer {
	return &SaramaProducer{producer: producer}
}

func (p *SaramaProducer) SendMessage(_ context.Context, topic, key string, payload []byte, headers map[string]string) error {
	var recordHeaders []sarama.RecordHeader
	for k, v := range headers {
		recordHeaders = append(recordHeaders, sarama.RecordHeader{
			Key:   []byte(k),
			Value: []byte(v),
		})
	}

	msg := &sarama.ProducerMessage{
		Topic:   topic,
		Key:     sarama.StringEncoder(key),
		Value:   sarama.ByteEncoder(payload),
		Headers: recordHeaders,
	}

	_, _, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("sarama send: %w", err)
	}
	return nil
}

func (p *SaramaProducer) Close() error {
	return p.producer.Close()
}

// NewKafkaProducer creates a sarama.SyncProducer with acks=all and wraps it
// behind the EventProducer facade.
func NewKafkaProducer(brokers []string, retries int) (domain.EventProducer, sarama.SyncProducer, error) {
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForAll
	kafkaConfig.Producer.Retry.Max = retries
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.Idempotent = true
	kafkaConfig.Net.MaxOpenRequests = 1

	raw, err := sarama.NewSyncProducer(brokers, kafkaConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create kafka producer: %w", err)
	}

	return NewSaramaProducer(raw), raw, nil
}
