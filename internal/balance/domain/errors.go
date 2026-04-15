package domain

import "errors"

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrInsufficientLocked  = errors.New("insufficient locked balance")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
	ErrBalanceNotFound     = errors.New("balance not found")
)
