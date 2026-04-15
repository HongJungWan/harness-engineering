package domain

import (
	"fmt"

	"github.com/shopspring/decimal"
)

type Money struct {
	Amount   decimal.Decimal
	Currency string
}

func NewMoney(amount decimal.Decimal, currency string) (Money, error) {
	if currency == "" {
		return Money{}, fmt.Errorf("currency must not be empty")
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func NewMoneyFromString(amount string, currency string) (Money, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return Money{}, fmt.Errorf("invalid amount %q: %w", amount, err)
	}
	return NewMoney(d, currency)
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: %s vs %s", m.Currency, other.Currency)
	}
	return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: %s vs %s", m.Currency, other.Currency)
	}
	return Money{Amount: m.Amount.Sub(other.Amount), Currency: m.Currency}, nil
}

func (m Money) Multiply(qty decimal.Decimal) Money {
	return Money{Amount: m.Amount.Mul(qty), Currency: m.Currency}
}

func (m Money) IsNegative() bool {
	return m.Amount.IsNegative()
}

func (m Money) IsZero() bool {
	return m.Amount.IsZero()
}

func (m Money) GreaterThanOrEqual(other Money) bool {
	return m.Amount.GreaterThanOrEqual(other.Amount)
}

func (m Money) String() string {
	return fmt.Sprintf("%s %s", m.Amount.String(), m.Currency)
}
