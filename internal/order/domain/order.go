package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type OrderSide string
type OrderType string
type OrderStatus string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"

	TypeLimit  OrderType = "LIMIT"
	TypeMarket OrderType = "MARKET"

	StatusPending         OrderStatus = "PENDING"
	StatusAccepted        OrderStatus = "ACCEPTED"
	StatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	StatusFilled          OrderStatus = "FILLED"
	StatusCancelled       OrderStatus = "CANCELLED"
	StatusRejected        OrderStatus = "REJECTED"
)

type Order struct {
	ID        string
	UserID    uint64
	Pair      shared.AssetPair
	Side      OrderSide
	OrderType OrderType
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	FilledQty decimal.Decimal
	Status    OrderStatus
	Reason    string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time

	events []shared.DomainEvent
}

func NewOrder(
	userID uint64,
	pair shared.AssetPair,
	side OrderSide,
	orderType OrderType,
	price decimal.Decimal,
	quantity decimal.Decimal,
) (*Order, error) {
	if quantity.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidQuantity
	}
	if orderType == TypeLimit && price.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidPrice
	}

	now := time.Now()
	return &Order{
		ID:        uuid.New().String(),
		UserID:    userID,
		Pair:      pair,
		Side:      side,
		OrderType: orderType,
		Price:     price,
		Quantity:  quantity,
		FilledQty: decimal.Zero,
		Status:    StatusPending,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (o *Order) Accept() error {
	if o.Status != StatusPending {
		return ErrInvalidTransition
	}
	o.Status = StatusAccepted
	o.UpdatedAt = time.Now()

	o.events = append(o.events, NewOrderPlacedEvent(o))
	return nil
}

func (o *Order) Fill(qty decimal.Decimal) error {
	if o.Status != StatusAccepted && o.Status != StatusPartiallyFilled {
		return ErrInvalidTransition
	}
	if qty.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidQuantity
	}

	newFilled := o.FilledQty.Add(qty)
	if newFilled.GreaterThan(o.Quantity) {
		return ErrFillExceedsQuantity
	}

	o.FilledQty = newFilled
	if o.FilledQty.Equal(o.Quantity) {
		o.Status = StatusFilled
		o.events = append(o.events, NewOrderFilledEvent(o))
	} else {
		o.Status = StatusPartiallyFilled
	}
	o.UpdatedAt = time.Now()
	return nil
}

func (o *Order) Cancel(reason string) error {
	if o.Status != StatusAccepted && o.Status != StatusPartiallyFilled {
		return ErrInvalidTransition
	}
	o.Status = StatusCancelled
	o.Reason = reason
	o.UpdatedAt = time.Now()

	o.events = append(o.events, NewOrderCancelledEvent(o))
	return nil
}

func (o *Order) Reject(reason string) error {
	if o.Status != StatusPending {
		return ErrInvalidTransition
	}
	o.Status = StatusRejected
	o.Reason = reason
	o.UpdatedAt = time.Now()
	return nil
}

func (o *Order) IsTerminal() bool {
	return o.Status == StatusFilled || o.Status == StatusCancelled || o.Status == StatusRejected
}

func (o *Order) RemainingQty() decimal.Decimal {
	return o.Quantity.Sub(o.FilledQty)
}

func (o *Order) RequiredAmount() decimal.Decimal {
	if o.Side == SideBuy {
		return o.Price.Mul(o.Quantity)
	}
	return o.Quantity
}

func (o *Order) LockedCurrency() string {
	if o.Side == SideBuy {
		return o.Pair.Quote
	}
	return o.Pair.Base
}

func (o *Order) PullEvents() []shared.DomainEvent {
	events := o.events
	o.events = nil
	return events
}
