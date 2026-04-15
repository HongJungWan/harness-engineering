package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	baldomain "github.com/HongJungWan/harness-engineering/internal/balance/domain"
	orderdomain "github.com/HongJungWan/harness-engineering/internal/order/domain"
	"github.com/HongJungWan/harness-engineering/internal/outbox"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type PlaceOrderCommand struct {
	IdempotencyKey string
	UserID         uint64
	Pair           shared.AssetPair
	Side           orderdomain.OrderSide
	OrderType      orderdomain.OrderType
	Price          decimal.Decimal
	Quantity       decimal.Decimal
}

type PlaceOrderResult struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type PlaceOrderUseCase struct {
	TxManager       shared.TxManager
	OrderRepo       orderdomain.OrderRepository
	BalanceRepo     baldomain.BalanceRepository
	IdempotencyRepo outbox.IdempotencyRepository
}

func NewPlaceOrderUseCase(
	txManager shared.TxManager,
	orderRepo orderdomain.OrderRepository,
	balanceRepo baldomain.BalanceRepository,
	idempotencyRepo outbox.IdempotencyRepository,
) *PlaceOrderUseCase {
	return &PlaceOrderUseCase{
		TxManager:       txManager,
		OrderRepo:       orderRepo,
		BalanceRepo:     balanceRepo,
		IdempotencyRepo: idempotencyRepo,
	}
}

// Execute implements the pessimistic lock transaction flow from 02_Code.md 3.1:
//
//  1. 멱등성 키 확인
//  2. Value Object 생성 + 필요 금액 계산
//  3. BEGIN TX
//     4. SELECT ... FROM balances WHERE user_id=? AND currency=? FOR UPDATE
//     5. balance.DeductAndLock(amount)
//     6. NewOrder → Accept()
//     7. balanceRepo.Save(tx, balance) → UPDATE balances + INSERT outbox_events
//     8. orderRepo.Save(tx, order)    → INSERT orders + INSERT outbox_events
//     9. INSERT idempotency_keys
//  10. COMMIT
func (uc *PlaceOrderUseCase) Execute(ctx context.Context, cmd PlaceOrderCommand) (*PlaceOrderResult, error) {
	// 1. 멱등성 키 확인 → 캐시 히트 시 저장된 응답 반환
	if cmd.IdempotencyKey != "" && uc.IdempotencyRepo != nil {
		cached, found, err := uc.IdempotencyRepo.Check(ctx, cmd.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("check idempotency: %w", err)
		}
		if found {
			var result PlaceOrderResult
			if err := json.Unmarshal(cached, &result); err != nil {
				return nil, fmt.Errorf("unmarshal cached response: %w", err)
			}
			return &result, nil
		}
	}

	// 2. 주문 생성 및 필요 금액 계산
	order, err := orderdomain.NewOrder(cmd.UserID, cmd.Pair, cmd.Side, cmd.OrderType, cmd.Price, cmd.Quantity)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

	requiredAmount := order.RequiredAmount()
	lockedCurrency := order.LockedCurrency()

	var result *PlaceOrderResult

	// 3~10. 트랜잭션 내 비관적 락 플로우
	err = uc.TxManager.RunInTx(ctx, func(txCtx context.Context) error {
		// 4. SELECT ... FOR UPDATE (잔고 행 비관적 잠금)
		balance, err := uc.BalanceRepo.FindByUserAndCurrencyForUpdate(txCtx, cmd.UserID, lockedCurrency)
		if err != nil {
			return fmt.Errorf("find balance: %w", err)
		}

		// 5. 잔고 차감 + 잠금
		if err := balance.DeductAndLock(requiredAmount, lockedCurrency, order.ID); err != nil {
			return err // ErrInsufficientBalance
		}

		// 6. 주문 접수 (PENDING → ACCEPTED, OrderPlaced 이벤트 발행)
		if err := order.Accept(); err != nil {
			return fmt.Errorf("accept order: %w", err)
		}

		// 7. 잔고 저장 (UPDATE balances + INSERT outbox_events: BalanceDeducted)
		if err := uc.BalanceRepo.Save(txCtx, balance); err != nil {
			return fmt.Errorf("save balance: %w", err)
		}

		// 8. 주문 저장 (INSERT orders + INSERT outbox_events: OrderPlaced)
		if err := uc.OrderRepo.Save(txCtx, order); err != nil {
			return fmt.Errorf("save order: %w", err)
		}

		result = &PlaceOrderResult{
			OrderID: order.ID,
			Status:  string(order.Status),
		}

		// 9. INSERT idempotency_keys
		if cmd.IdempotencyKey != "" && uc.IdempotencyRepo != nil {
			respBody, _ := json.Marshal(result)
			if err := uc.IdempotencyRepo.Save(txCtx, cmd.IdempotencyKey, respBody); err != nil {
				return fmt.Errorf("save idempotency key: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}
