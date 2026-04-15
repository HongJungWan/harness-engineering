package domain

import (
	"time"

	"github.com/shopspring/decimal"

	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type Balance struct {
	ID        uint64
	UserID    uint64
	Currency  string
	Available decimal.Decimal
	Locked    decimal.Decimal
	Version   int64
	UpdatedAt time.Time

	events []shared.DomainEvent
}

func (b *Balance) DeductAndLock(amount decimal.Decimal, currency string, refID string) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAmount
	}
	if b.Available.LessThan(amount) {
		return ErrInsufficientBalance
	}

	b.Available = b.Available.Sub(amount)
	b.Locked = b.Locked.Add(amount)
	b.UpdatedAt = time.Now()

	b.events = append(b.events, NewBalanceDeductedEvent(b, amount, currency, refID))
	return nil
}

func (b *Balance) Unlock(amount decimal.Decimal, currency string, refID string) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAmount
	}
	if b.Locked.LessThan(amount) {
		return ErrInsufficientLocked
	}

	b.Locked = b.Locked.Sub(amount)
	b.Available = b.Available.Add(amount)
	b.UpdatedAt = time.Now()

	b.events = append(b.events, NewBalanceRestoredEvent(b, amount, currency, refID))
	return nil
}

func (b *Balance) SettleFill(amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAmount
	}
	if b.Locked.LessThan(amount) {
		return ErrInsufficientLocked
	}

	b.Locked = b.Locked.Sub(amount)
	b.UpdatedAt = time.Now()
	return nil
}

func (b *Balance) PullEvents() []shared.DomainEvent {
	events := b.events
	b.events = nil
	return events
}
