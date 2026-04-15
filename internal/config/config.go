package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Kafka    KafkaConfig
}

type AppConfig struct {
	Name             string        `env:"APP_NAME"             envDefault:"harness-order"`
	Port             int           `env:"APP_PORT"             envDefault:"8080"`
	GracefulTimeout  time.Duration `env:"APP_GRACEFUL_TIMEOUT" envDefault:"15s"`
	RelayWorkerCount int           `env:"RELAY_WORKER_COUNT"   envDefault:"2"`
	RelayPollInterval time.Duration `env:"RELAY_POLL_INTERVAL" envDefault:"50ms"`
	RelayBatchSize   int           `env:"RELAY_BATCH_SIZE"     envDefault:"100"`
	RelayMaxRetries  int           `env:"RELAY_MAX_RETRIES"    envDefault:"5"`
	RelayBackoffBase time.Duration `env:"RELAY_BACKOFF_BASE"   envDefault:"1s"`
}

type DatabaseConfig struct {
	Host            string        `env:"DB_HOST"              envDefault:"localhost"`
	Port            int           `env:"DB_PORT"              envDefault:"3306"`
	User            string        `env:"DB_USER"              envDefault:"harness"`
	Password        string        `env:"DB_PASSWORD"          envDefault:"harness"`
	DBName          string        `env:"DB_NAME"              envDefault:"harness"`
	MaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS"    envDefault:"50"`
	MaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS"    envDefault:"25"`
	ConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"5m"`
	ConnMaxIdleTime time.Duration `env:"DB_CONN_MAX_IDLE"     envDefault:"1m"`
	ReadTimeout     time.Duration `env:"DB_READ_TIMEOUT"      envDefault:"3s"`
	WriteTimeout    time.Duration `env:"DB_WRITE_TIMEOUT"     envDefault:"3s"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Asia%%2FSeoul&readTimeout=%s&writeTimeout=%s&interpolateParams=true",
		d.User, d.Password, d.Host, d.Port, d.DBName,
		d.ReadTimeout, d.WriteTimeout,
	)
}

type KafkaConfig struct {
	Brokers        []string `env:"KAFKA_BROKERS"         envDefault:"localhost:9092" envSeparator:","`
	OrderTopic     string   `env:"KAFKA_ORDER_TOPIC"     envDefault:"order.events"`
	DLQTopic       string   `env:"KAFKA_DLQ_TOPIC"       envDefault:"order.events.dlq"`
	ConsumerGroup  string   `env:"KAFKA_CONSUMER_GROUP"  envDefault:"order-processor"`
	ProducerAcks   string   `env:"KAFKA_PRODUCER_ACKS"   envDefault:"all"`
	ProducerRetries int     `env:"KAFKA_PRODUCER_RETRIES" envDefault:"3"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS (%d) must be <= DB_MAX_OPEN_CONNS (%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns)
	}
	if c.App.RelayBatchSize < 1 || c.App.RelayBatchSize > 1000 {
		return fmt.Errorf("RELAY_BATCH_SIZE must be between 1 and 1000, got %d",
			c.App.RelayBatchSize)
	}
	return nil
}
