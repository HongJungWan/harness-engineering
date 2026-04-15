package domain

import "errors"

var (
	ErrInvalidQuantity    = errors.New("quantity must be greater than zero")
	ErrInvalidPrice       = errors.New("price must be greater than zero for LIMIT orders")
	ErrInvalidTransition  = errors.New("invalid order state transition")
	ErrFillExceedsQuantity = errors.New("fill quantity exceeds order quantity")
	ErrOrderNotFound      = errors.New("order not found")
)
