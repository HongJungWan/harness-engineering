package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const balanceEventTopic = "balance.events"

type BalanceDeductedEvent struct {
	ID             string    `json:"event_id"`
	BalanceID      uint64    `json:"balance_id"`
	UserID         uint64    `json:"user_id"`
	Currency       string    `json:"currency"`
	Amount         string    `json:"amount"`
	ReferenceID    string    `json:"reference_id"`
	AvailableAfter string    `json:"available_after"`
	LockedAfter    string    `json:"locked_after"`
	OccurredTs     time.Time `json:"occurred_at"`
}

func NewBalanceDeductedEvent(b *Balance, amount decimal.Decimal, currency, refID string) *BalanceDeductedEvent {
	return &BalanceDeductedEvent{
		ID:             uuid.New().String(),
		BalanceID:      b.ID,
		UserID:         b.UserID,
		Currency:       currency,
		Amount:         amount.String(),
		ReferenceID:    refID,
		AvailableAfter: b.Available.String(),
		LockedAfter:    b.Locked.String(),
		OccurredTs:     time.Now(),
	}
}

func (e *BalanceDeductedEvent) EventID() string         { return e.ID }
func (e *BalanceDeductedEvent) EventType() string        { return "BalanceDeducted" }
func (e *BalanceDeductedEvent) AggregateType() string    { return "Balance" }
func (e *BalanceDeductedEvent) AggregateID() string      { return fmt.Sprintf("%d", e.BalanceID) }
func (e *BalanceDeductedEvent) OccurredAt() time.Time    { return e.OccurredTs }
func (e *BalanceDeductedEvent) KafkaTopic() string       { return balanceEventTopic }
func (e *BalanceDeductedEvent) KafkaKey() string         { return fmt.Sprintf("%d", e.BalanceID) }
func (e *BalanceDeductedEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type BalanceRestoredEvent struct {
	ID             string    `json:"event_id"`
	BalanceID      uint64    `json:"balance_id"`
	UserID         uint64    `json:"user_id"`
	Currency       string    `json:"currency"`
	Amount         string    `json:"amount"`
	ReferenceID    string    `json:"reference_id"`
	AvailableAfter string    `json:"available_after"`
	LockedAfter    string    `json:"locked_after"`
	OccurredTs     time.Time `json:"occurred_at"`
}

func NewBalanceRestoredEvent(b *Balance, amount decimal.Decimal, currency, refID string) *BalanceRestoredEvent {
	return &BalanceRestoredEvent{
		ID:             uuid.New().String(),
		BalanceID:      b.ID,
		UserID:         b.UserID,
		Currency:       currency,
		Amount:         amount.String(),
		ReferenceID:    refID,
		AvailableAfter: b.Available.String(),
		LockedAfter:    b.Locked.String(),
		OccurredTs:     time.Now(),
	}
}

func (e *BalanceRestoredEvent) EventID() string         { return e.ID }
func (e *BalanceRestoredEvent) EventType() string        { return "BalanceRestored" }
func (e *BalanceRestoredEvent) AggregateType() string    { return "Balance" }
func (e *BalanceRestoredEvent) AggregateID() string      { return fmt.Sprintf("%d", e.BalanceID) }
func (e *BalanceRestoredEvent) OccurredAt() time.Time    { return e.OccurredTs }
func (e *BalanceRestoredEvent) KafkaTopic() string       { return balanceEventTopic }
func (e *BalanceRestoredEvent) KafkaKey() string         { return fmt.Sprintf("%d", e.BalanceID) }
func (e *BalanceRestoredEvent) Payload() ([]byte, error) { return json.Marshal(e) }
