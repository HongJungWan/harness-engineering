package domain

import "context"

type OrderRepository interface {
	Save(ctx context.Context, order *Order) error
	Update(ctx context.Context, order *Order) error
	FindByID(ctx context.Context, id string) (*Order, error)
	FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*Order, error)
}
