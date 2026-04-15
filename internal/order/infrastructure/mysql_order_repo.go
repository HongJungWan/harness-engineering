package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"

	domain "github.com/HongJungWan/harness-engineering/internal/order/domain"
	"github.com/HongJungWan/harness-engineering/internal/outbox"
	shareddomain "github.com/HongJungWan/harness-engineering/internal/shared/domain"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

type orderRow struct {
	ID        int64           `db:"id"`
	OrderUID  string          `db:"order_uid"`
	UserID    uint64          `db:"user_id"`
	Pair      string          `db:"pair"`
	Side      string          `db:"side"`
	OrderType string          `db:"order_type"`
	Price     decimal.Decimal `db:"price"`
	Quantity  decimal.Decimal `db:"quantity"`
	FilledQty decimal.Decimal `db:"filled_qty"`
	Status    string          `db:"status"`
	Reason    sql.NullString  `db:"reason"`
	Version   int64           `db:"version"`
	CreatedAt time.Time       `db:"created_at"`
	UpdatedAt time.Time       `db:"updated_at"`
}

type MysqlOrderRepository struct {
	DB       *sqlx.DB
	OutboxRepo outbox.OutboxRepository
}

func NewMysqlOrderRepository(db *sqlx.DB, outboxRepo outbox.OutboxRepository) domain.OrderRepository {
	return &MysqlOrderRepository{DB: db, OutboxRepo: outboxRepo}
}

func (r *MysqlOrderRepository) Save(ctx context.Context, order *domain.Order) error {
	tx, ok := shared.ExtractTx(ctx)
	if !ok {
		return fmt.Errorf("save order: no transaction in context")
	}

	query := `INSERT INTO orders (order_uid, user_id, pair, side, order_type, price, quantity, filled_qty, status, reason, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var reason sql.NullString
	if order.Reason != "" {
		reason = sql.NullString{String: order.Reason, Valid: true}
	}

	_, err := tx.ExecContext(ctx, query,
		order.ID, order.UserID, order.Pair.String(), string(order.Side), string(order.OrderType),
		order.Price, order.Quantity, order.FilledQty, string(order.Status),
		reason, order.Version, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	events := order.PullEvents()
	for _, evt := range events {
		payload, err := evt.Payload()
		if err != nil {
			return fmt.Errorf("marshal order event: %w", err)
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
			return fmt.Errorf("insert order outbox event: %w", err)
		}
	}

	return nil
}

func (r *MysqlOrderRepository) Update(ctx context.Context, order *domain.Order) error {
	tx, ok := shared.ExtractTx(ctx)
	if !ok {
		return fmt.Errorf("update order: no transaction in context")
	}

	var reason sql.NullString
	if order.Reason != "" {
		reason = sql.NullString{String: order.Reason, Valid: true}
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = ?, filled_qty = ?, reason = ?, version = version + 1, updated_at = ?
		 WHERE order_uid = ? AND version = ?`,
		string(order.Status), order.FilledQty, reason, order.UpdatedAt,
		order.ID, order.Version)
	if err != nil {
		return fmt.Errorf("update order: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("update order: optimistic lock conflict")
	}

	events := order.PullEvents()
	for _, evt := range events {
		payload, err := evt.Payload()
		if err != nil {
			return fmt.Errorf("marshal order event: %w", err)
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
			return fmt.Errorf("insert order outbox event: %w", err)
		}
	}

	return nil
}

func (r *MysqlOrderRepository) FindByID(ctx context.Context, id string) (*domain.Order, error) {
	var row orderRow
	err := r.DB.GetContext(ctx, &row, `SELECT * FROM orders WHERE order_uid = ?`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, fmt.Errorf("find order by id: %w", err)
	}
	return rowToOrder(&row), nil
}

func (r *MysqlOrderRepository) FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*domain.Order, error) {
	var rows []orderRow
	err := r.DB.SelectContext(ctx, &rows,
		`SELECT * FROM orders WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("find orders by user: %w", err)
	}

	orders := make([]*domain.Order, len(rows))
	for i, row := range rows {
		orders[i] = rowToOrder(&row)
	}
	return orders, nil
}

func rowToOrder(row *orderRow) *domain.Order {
	pair, _ := shareddomain.NewAssetPairFromString(row.Pair)
	reason := ""
	if row.Reason.Valid {
		reason = row.Reason.String
	}

	return &domain.Order{
		ID:        row.OrderUID,
		UserID:    row.UserID,
		Pair:      pair,
		Side:      domain.OrderSide(row.Side),
		OrderType: domain.OrderType(row.OrderType),
		Price:     row.Price,
		Quantity:  row.Quantity,
		FilledQty: row.FilledQty,
		Status:    domain.OrderStatus(row.Status),
		Reason:    reason,
		Version:   row.Version,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
