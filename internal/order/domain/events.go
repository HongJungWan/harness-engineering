package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const orderEventTopic = "order.events"

type OrderPlacedEvent struct {
	ID         string    `json:"event_id"`
	OrderID    string    `json:"order_id"`
	UserID     uint64    `json:"user_id"`
	Pair       string    `json:"pair"`
	Side       string    `json:"side"`
	Type       string    `json:"order_type"`
	Price      string    `json:"price"`
	Quantity   string    `json:"quantity"`
	Status     string    `json:"status"`
	OccurredTs time.Time `json:"occurred_at"`
}

func NewOrderPlacedEvent(o *Order) *OrderPlacedEvent {
	return &OrderPlacedEvent{
		ID:         uuid.New().String(),
		OrderID:    o.ID,
		UserID:     o.UserID,
		Pair:       o.Pair.String(),
		Side:       string(o.Side),
		Type:       string(o.OrderType),
		Price:      o.Price.String(),
		Quantity:   o.Quantity.String(),
		Status:     string(o.Status),
		OccurredTs: time.Now(),
	}
}

func (e *OrderPlacedEvent) EventID() string        { return e.ID }
func (e *OrderPlacedEvent) EventType() string       { return "OrderPlaced" }
func (e *OrderPlacedEvent) AggregateType() string   { return "Order" }
func (e *OrderPlacedEvent) AggregateID() string     { return e.OrderID }
func (e *OrderPlacedEvent) OccurredAt() time.Time   { return e.OccurredTs }
func (e *OrderPlacedEvent) KafkaTopic() string      { return orderEventTopic }
func (e *OrderPlacedEvent) KafkaKey() string         { return e.OrderID }
func (e *OrderPlacedEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type OrderCancelledEvent struct {
	ID          string    `json:"event_id"`
	OrderID     string    `json:"order_id"`
	UserID      uint64    `json:"user_id"`
	Reason      string    `json:"reason"`
	CancelledAt time.Time `json:"cancelled_at"`
}

func NewOrderCancelledEvent(o *Order) *OrderCancelledEvent {
	return &OrderCancelledEvent{
		ID:          uuid.New().String(),
		OrderID:     o.ID,
		UserID:      o.UserID,
		Reason:      o.Reason,
		CancelledAt: time.Now(),
	}
}

func (e *OrderCancelledEvent) EventID() string         { return e.ID }
func (e *OrderCancelledEvent) EventType() string        { return "OrderCancelled" }
func (e *OrderCancelledEvent) AggregateType() string    { return "Order" }
func (e *OrderCancelledEvent) AggregateID() string      { return e.OrderID }
func (e *OrderCancelledEvent) OccurredAt() time.Time    { return e.CancelledAt }
func (e *OrderCancelledEvent) KafkaTopic() string       { return orderEventTopic }
func (e *OrderCancelledEvent) KafkaKey() string         { return e.OrderID }
func (e *OrderCancelledEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type OrderFilledEvent struct {
	ID         string    `json:"event_id"`
	OrderID    string    `json:"order_id"`
	UserID     uint64    `json:"user_id"`
	FilledQty  string    `json:"filled_qty"`
	Quantity   string    `json:"quantity"`
	FilledAt   time.Time `json:"filled_at"`
}

func NewOrderFilledEvent(o *Order) *OrderFilledEvent {
	return &OrderFilledEvent{
		ID:        uuid.New().String(),
		OrderID:   o.ID,
		UserID:    o.UserID,
		FilledQty: o.FilledQty.String(),
		Quantity:  o.Quantity.String(),
		FilledAt:  time.Now(),
	}
}

func (e *OrderFilledEvent) EventID() string         { return e.ID }
func (e *OrderFilledEvent) EventType() string        { return "OrderFilled" }
func (e *OrderFilledEvent) AggregateType() string    { return "Order" }
func (e *OrderFilledEvent) AggregateID() string      { return e.OrderID }
func (e *OrderFilledEvent) OccurredAt() time.Time    { return e.FilledAt }
func (e *OrderFilledEvent) KafkaTopic() string       { return orderEventTopic }
func (e *OrderFilledEvent) KafkaKey() string         { return e.OrderID }
func (e *OrderFilledEvent) Payload() ([]byte, error) { return json.Marshal(e) }
