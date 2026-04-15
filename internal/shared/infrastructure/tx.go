package infrastructure

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type ctxKey string

const txKey ctxKey = "tx"

type SqlxTxManager struct {
	DB *sqlx.DB
}

func NewTxManager(db *sqlx.DB) shared.TxManager {
	return &SqlxTxManager{DB: db}
}

func (m *SqlxTxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.DB.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	txCtx := context.WithValue(ctx, txKey, tx)
	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func ExtractTx(ctx context.Context) (*sqlx.Tx, bool) {
	tx, ok := ctx.Value(txKey).(*sqlx.Tx)
	return tx, ok
}
