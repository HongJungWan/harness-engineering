package application

import (
	"context"
	"fmt"

	baldomain "github.com/HongJungWan/harness-engineering/internal/balance/domain"
	orderdomain "github.com/HongJungWan/harness-engineering/internal/order/domain"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

type CancelOrderCommand struct {
	OrderID string
	Reason  string
}

type CancelOrderResult struct {
	OrderID string
	Status  string
}

type CancelOrderUseCase struct {
	TxManager   shared.TxManager
	OrderRepo   orderdomain.OrderRepository
	BalanceRepo baldomain.BalanceRepository
}

func NewCancelOrderUseCase(
	txManager shared.TxManager,
	orderRepo orderdomain.OrderRepository,
	balanceRepo baldomain.BalanceRepository,
) *CancelOrderUseCase {
	return &CancelOrderUseCase{
		TxManager:   txManager,
		OrderRepo:   orderRepo,
		BalanceRepo: balanceRepo,
	}
}

// Execute cancels an order and restores the locked balance atomically.
//
// Flow:
//  1. Find order by ID
//  2. BEGIN TX
//     3. SELECT ... FROM balances WHERE user_id=? AND currency=? FOR UPDATE
//     4. order.Cancel()
//     5. balance.Unlock(lockedAmount)
//     6. balanceRepo.Save → UPDATE balances + INSERT outbox (BalanceRestored)
//     7. orderRepo.Update → UPDATE orders + INSERT outbox (OrderCancelled)
//  8. COMMIT
func (uc *CancelOrderUseCase) Execute(ctx context.Context, cmd CancelOrderCommand) (*CancelOrderResult, error) {
	// 1. 주문 조회
	order, err := uc.OrderRepo.FindByID(ctx, cmd.OrderID)
	if err != nil {
		return nil, fmt.Errorf("find order: %w", err)
	}

	// 이미 취소된 주문은 멱등하게 처리
	if order.Status == orderdomain.StatusCancelled {
		return &CancelOrderResult{OrderID: order.ID, Status: string(order.Status)}, nil
	}

	lockedCurrency := order.LockedCurrency()
	lockedAmount := order.RemainingQty()
	if order.Side == orderdomain.SideBuy {
		lockedAmount = order.Price.Mul(order.RemainingQty())
	}

	var result *CancelOrderResult

	err = uc.TxManager.RunInTx(ctx, func(txCtx context.Context) error {
		// 3. 잔고 행 비관적 잠금
		balance, err := uc.BalanceRepo.FindByUserAndCurrencyForUpdate(txCtx, order.UserID, lockedCurrency)
		if err != nil {
			return fmt.Errorf("find balance: %w", err)
		}

		// 4. 주문 취소 (ACCEPTED → CANCELLED, OrderCancelled 이벤트)
		if err := order.Cancel(cmd.Reason); err != nil {
			return fmt.Errorf("cancel order: %w", err)
		}

		// 5. 잔고 잠금 해제 (locked → available, BalanceRestored 이벤트)
		if err := balance.Unlock(lockedAmount, lockedCurrency, order.ID); err != nil {
			return fmt.Errorf("unlock balance: %w", err)
		}

		// 6. 잔고 저장
		if err := uc.BalanceRepo.Save(txCtx, balance); err != nil {
			return fmt.Errorf("save balance: %w", err)
		}

		// 7. 주문 업데이트
		if err := uc.OrderRepo.Update(txCtx, order); err != nil {
			return fmt.Errorf("update order: %w", err)
		}

		result = &CancelOrderResult{
			OrderID: order.ID,
			Status:  string(order.Status),
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}
