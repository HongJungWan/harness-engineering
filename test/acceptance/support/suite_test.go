package support

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	balanceinfra "github.com/HongJungWan/harness-engineering/internal/balance/infrastructure"
	"github.com/HongJungWan/harness-engineering/internal/order/application"
	orderinfra "github.com/HongJungWan/harness-engineering/internal/order/infrastructure"
	"github.com/HongJungWan/harness-engineering/internal/outbox"
	sharedinfra "github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

var (
	TestDB         *sqlx.DB
	PlaceOrderUC   *application.PlaceOrderUseCase
	CancelOrderUC  *application.CancelOrderUseCase
	OutboxRepo     outbox.OutboxRepository
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "harness:harness@tcp(localhost:3306)/harness?charset=utf8mb4&parseTime=true&loc=Local&interpolateParams=true"
	}

	var err error
	TestDB, err = sqlx.Connect("mysql", dsn)
	if err != nil {
		log.Fatalf("failed to connect to test database: %v", err)
	}
	defer TestDB.Close()

	// Wire dependencies
	txManager := sharedinfra.NewTxManager(TestDB)
	OutboxRepo = outbox.NewMysqlOutboxRepository(TestDB)
	idempotencyRepo := outbox.NewMysqlIdempotencyRepository(TestDB)
	orderRepo := orderinfra.NewMysqlOrderRepository(TestDB, OutboxRepo)
	balanceRepo := balanceinfra.NewMysqlBalanceRepository(TestDB, OutboxRepo)

	PlaceOrderUC = application.NewPlaceOrderUseCase(txManager, orderRepo, balanceRepo, idempotencyRepo)
	CancelOrderUC = application.NewCancelOrderUseCase(txManager, orderRepo, balanceRepo)

	os.Exit(m.Run())
}

func CleanAll() error {
	ctx := context.Background()
	tables := []string{"outbox_events", "orders", "idempotency_keys", "processed_events", "balances"}
	for _, t := range tables {
		if _, err := TestDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", t)); err != nil {
			return fmt.Errorf("clean table %s: %w", t, err)
		}
	}
	return nil
}

func SeedBalance(userID uint64, currency string, available string) error {
	_, err := TestDB.ExecContext(context.Background(),
		`INSERT INTO balances (user_id, currency, available, locked, version)
		 VALUES (?, ?, ?, 0, 1)
		 ON DUPLICATE KEY UPDATE available = ?, locked = 0, version = 1`,
		userID, currency, available, available)
	return err
}

func GetBalance(userID uint64, currency string) (available, locked string, err error) {
	err = TestDB.QueryRowContext(context.Background(),
		`SELECT CAST(available AS CHAR), CAST(locked AS CHAR) FROM balances WHERE user_id = ? AND currency = ?`,
		userID, currency).Scan(&available, &locked)
	return
}

func CountOutboxEvents(eventType string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM outbox_events`
	if eventType != "" {
		query += fmt.Sprintf(` WHERE event_type = '%s'`, eventType)
	}
	err := TestDB.QueryRowContext(context.Background(), query).Scan(&count)
	return count, err
}

func CountAllOutboxEvents() (int, error) {
	return CountOutboxEvents("")
}

func CountPendingOutboxEvents() (int, error) {
	var count int
	err := TestDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM outbox_events WHERE status = 'PENDING'`).Scan(&count)
	return count, err
}

func GetOutboxEventIDs() ([]int64, error) {
	var ids []int64
	err := TestDB.SelectContext(context.Background(), &ids,
		`SELECT id FROM outbox_events ORDER BY id ASC`)
	return ids, err
}

func MarkOutboxEventsSent(ids []int64) error {
	return OutboxRepo.MarkSent(context.Background(), ids)
}

func CountSentOutboxEvents() (int, error) {
	var count int
	err := TestDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM outbox_events WHERE status = 'SENT'`).Scan(&count)
	return count, err
}

func CountProcessedEvents(eventID, consumerGroup string) (int, error) {
	var count int
	err := TestDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM processed_events WHERE event_id = ? AND consumer_group = ?`,
		eventID, consumerGroup).Scan(&count)
	return count, err
}

func InsertProcessedEvent(eventID, consumerGroup string) error {
	_, err := TestDB.ExecContext(context.Background(),
		`INSERT IGNORE INTO processed_events (event_id, consumer_group) VALUES (?, ?)`,
		eventID, consumerGroup)
	return err
}
