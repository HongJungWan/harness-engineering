package presentation

import "time"

type PlaceOrderRequest struct {
	UserID    uint64 `json:"user_id"`
	Base      string `json:"base"`
	Quote     string `json:"quote"`
	Side      string `json:"side"`
	OrderType string `json:"order_type"`
	Price     string `json:"price"`
	Quantity  string `json:"quantity"`
}

type PlaceOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type CancelOrderRequest struct {
	Reason string `json:"reason"`
}

type CancelOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type OrderResponse struct {
	OrderID   string    `json:"order_id"`
	UserID    uint64    `json:"user_id"`
	Pair      string    `json:"pair"`
	Side      string    `json:"side"`
	OrderType string    `json:"order_type"`
	Price     string    `json:"price"`
	Quantity  string    `json:"quantity"`
	FilledQty string    `json:"filled_qty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type BalanceResponse struct {
	Currency  string `json:"currency"`
	Available string `json:"available"`
	Locked    string `json:"locked"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
