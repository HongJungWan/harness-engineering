package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"

	domain "github.com/HongJungWan/harness-engineering/internal/balance/domain"
	"github.com/HongJungWan/harness-engineering/internal/outbox"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

type balanceRow struct {
	ID        uint64          `db:"id"`
	UserID    uint64          `db:"user_id"`
	Currency  string          `db:"currency"`
	Available decimal.Decimal `db:"available"`
	Locked    decimal.Decimal `db:"locked"`
	Version   int64           `db:"version"`
	UpdatedAt time.Time       `db:"updated_at"`
}

type MysqlBalanceRepository struct {
	DB         *sqlx.DB
	OutboxRepo outbox.OutboxRepository
}

func NewMysqlBalanceRepository(db *sqlx.DB, outboxRepo outbox.OutboxRepository) domain.BalanceRepository {
	return &MysqlBalanceRepository{DB: db, OutboxRepo: outboxRepo}
}

func (r *MysqlBalanceRepository) FindByUserAndCurrencyForUpdate(ctx context.Context, userID uint64, currency string) (*domain.Balance, error) {
	tx, ok := shared.ExtractTx(ctx)
	if !ok {
		return nil, fmt.Errorf("find balance for update: no transaction in context")
	}

	var row balanceRow
	err := tx.GetContext(ctx, &row,
		`SELECT id, user_id, currency, available, locked, version, updated_at
		 FROM balances
		 WHERE user_id = ? AND currency = ?
		 FOR UPDATE`,
		userID, currency)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrBalanceNotFound
		}
		return nil, fmt.Errorf("find balance for update: %w", err)
	}

	return &domain.Balance{
		ID:        row.ID,
		UserID:    row.UserID,
		Currency:  row.Currency,
		Available: row.Available,
		Locked:    row.Locked,
		Version:   row.Version,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *MysqlBalanceRepository) Save(ctx context.Context, balance *domain.Balance) error {
	tx, ok := shared.ExtractTx(ctx)
	if !ok {
		return fmt.Errorf("save balance: no transaction in context")
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE balances SET available = ?, locked = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?`,
		balance.Available, balance.Locked, time.Now(),
		balance.ID, balance.Version)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("update balance: optimistic lock conflict")
	}

	events := balance.PullEvents()
	for _, evt := range events {
		payload, err := evt.Payload()
		if err != nil {
			return fmt.Errorf("marshal balance event: %w", err)
		}
		if err := r.OutboxRepo.InsertEvent(ctx, &outbox.Event{
			EventID:       evt.EventID(),
			AggregateType: evt.AggregateType(),
			AggregateID:   evt.AggregateID(),
			EventType:     evt.EventType(),
			KafkaTopic:    evt.KafkaTopic(),
			KafkaKey:      evt.KafkaKey(),
			Payload:       payload,
		}); err != nil {
			return fmt.Errorf("insert balance outbox event: %w", err)
		}
	}

	return nil
}

func (r *MysqlBalanceRepository) FindByUser(ctx context.Context, userID uint64) ([]*domain.Balance, error) {
	var rows []balanceRow
	err := r.DB.SelectContext(ctx, &rows,
		`SELECT id, user_id, currency, available, locked, version, updated_at
		 FROM balances WHERE user_id = ?`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("find balances by user: %w", err)
	}

	balances := make([]*domain.Balance, len(rows))
	for i, row := range rows {
		balances[i] = &domain.Balance{
			ID:        row.ID,
			UserID:    row.UserID,
			Currency:  row.Currency,
			Available: row.Available,
			Locked:    row.Locked,
			Version:   row.Version,
			UpdatedAt: row.UpdatedAt,
		}
	}
	return balances, nil
}
