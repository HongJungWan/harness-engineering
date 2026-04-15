package presentation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"

	baldomain "github.com/HongJungWan/harness-engineering/internal/balance/domain"
	"github.com/HongJungWan/harness-engineering/internal/order/application"
	orderdomain "github.com/HongJungWan/harness-engineering/internal/order/domain"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type Handler struct {
	PlaceOrder  *application.PlaceOrderUseCase
	CancelOrder *application.CancelOrderUseCase
	OrderRepo   orderdomain.OrderRepository
	BalanceRepo baldomain.BalanceRepository
}

func NewHandler(
	placeOrder *application.PlaceOrderUseCase,
	cancelOrder *application.CancelOrderUseCase,
	orderRepo orderdomain.OrderRepository,
	balanceRepo baldomain.BalanceRepository,
) *Handler {
	return &Handler{
		PlaceOrder:  placeOrder,
		CancelOrder: cancelOrder,
		OrderRepo:   orderRepo,
		BalanceRepo: balanceRepo,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/orders", h.HandlePlaceOrder)
	r.Delete("/api/v1/orders/{orderID}", h.HandleCancelOrder)
	r.Get("/api/v1/orders/{orderID}", h.HandleGetOrder)
	r.Get("/api/v1/users/{userID}/balances", h.HandleGetBalances)
}

func (h *Handler) HandlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var req PlaceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	pair, err := shared.NewAssetPair(req.Base, req.Quote)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	price, err := decimal.NewFromString(req.Price)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid price"})
		return
	}

	qty, err := decimal.NewFromString(req.Quantity)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid quantity"})
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")

	result, err := h.PlaceOrder.Execute(r.Context(), application.PlaceOrderCommand{
		IdempotencyKey: idempotencyKey,
		UserID:         req.UserID,
		Pair:           pair,
		Side:           orderdomain.OrderSide(req.Side),
		OrderType:      orderdomain.OrderType(req.OrderType),
		Price:          price,
		Quantity:       qty,
	})
	if err != nil {
		if errors.Is(err, baldomain.ErrInsufficientBalance) {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "insufficient balance"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, PlaceOrderResponse{
		OrderID: result.OrderID,
		Status:  result.Status,
	})
}

func (h *Handler) HandleCancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")

	var req CancelOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "user_requested"
	}

	result, err := h.CancelOrder.Execute(r.Context(), application.CancelOrderCommand{
		OrderID: orderID,
		Reason:  req.Reason,
	})
	if err != nil {
		if errors.Is(err, orderdomain.ErrOrderNotFound) {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "order not found"})
			return
		}
		if errors.Is(err, orderdomain.ErrInvalidTransition) {
			writeJSON(w, http.StatusConflict, ErrorResponse{Error: "order cannot be cancelled"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, CancelOrderResponse{
		OrderID: result.OrderID,
		Status:  result.Status,
	})
}

func (h *Handler) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")

	order, err := h.OrderRepo.FindByID(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, orderdomain.ErrOrderNotFound) {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "order not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, OrderResponse{
		OrderID:   order.ID,
		UserID:    order.UserID,
		Pair:      order.Pair.String(),
		Side:      string(order.Side),
		OrderType: string(order.OrderType),
		Price:     order.Price.String(),
		Quantity:  order.Quantity.String(),
		FilledQty: order.FilledQty.String(),
		Status:    string(order.Status),
		CreatedAt: order.CreatedAt,
	})
}

func (h *Handler) HandleGetBalances(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "userID")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid user_id"})
		return
	}

	balances, err := h.BalanceRepo.FindByUser(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	resp := make([]BalanceResponse, len(balances))
	for i, b := range balances {
		resp[i] = BalanceResponse{
			Currency:  b.Currency,
			Available: b.Available.String(),
			Locked:    b.Locked.String(),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
