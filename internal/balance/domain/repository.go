package domain

import "context"

type BalanceRepository interface {
	FindByUserAndCurrencyForUpdate(ctx context.Context, userID uint64, currency string) (*Balance, error)
	Save(ctx context.Context, balance *Balance) error
	FindByUser(ctx context.Context, userID uint64) ([]*Balance, error)
}
