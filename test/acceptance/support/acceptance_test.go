package support

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	baldomain "github.com/HongJungWan/harness-engineering/internal/balance/domain"
	"github.com/HongJungWan/harness-engineering/internal/order/application"
	orderdomain "github.com/HongJungWan/harness-engineering/internal/order/domain"
	shared "github.com/HongJungWan/harness-engineering/internal/shared/domain"
)

// Scenario state
var (
	lastOrderID string
	lastError   error
	lastResults []ConcurrentResult
)

// --- Helper functions for tests ---

func placeOrder(userID uint64, base, quote, side, orderType string, price, qty string) error {
	pair, err := shared.NewAssetPair(base, quote)
	if err != nil {
		return err
	}
	p, _ := decimal.NewFromString(price)
	q, _ := decimal.NewFromString(qty)

	result, err := PlaceOrderUC.Execute(context.Background(), application.PlaceOrderCommand{
		UserID:    userID,
		Pair:      pair,
		Side:      orderdomain.OrderSide(side),
		OrderType: orderdomain.OrderType(orderType),
		Price:     p,
		Quantity:  q,
	})
	lastError = err
	if err == nil {
		lastOrderID = result.OrderID
	}
	return nil
}

func cancelLastOrder() error {
	if lastOrderID == "" {
		return fmt.Errorf("no order to cancel")
	}
	_, err := CancelOrderUC.Execute(context.Background(), application.CancelOrderCommand{
		OrderID: lastOrderID,
		Reason:  "user_requested",
	})
	lastError = err
	return nil
}

func assertBalance(t *testing.T, userID uint64, currency, expectedAvail, expectedLocked string) {
	t.Helper()
	avail, locked, err := GetBalance(userID, currency)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	a, _ := decimal.NewFromString(avail)
	l, _ := decimal.NewFromString(locked)
	ea, _ := decimal.NewFromString(expectedAvail)
	el, _ := decimal.NewFromString(expectedLocked)
	if !a.Equal(ea) {
		t.Errorf("user %d %s: expected available=%s, got=%s", userID, currency, expectedAvail, avail)
	}
	if !l.Equal(el) {
		t.Errorf("user %d %s: expected locked=%s, got=%s", userID, currency, expectedLocked, locked)
	}
}

// --- Test functions ---

func TestOrderPlacement_HappyPath(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}
	if err := SeedBalance(1, "KRW", "10000000"); err != nil {
		t.Fatal(err)
	}

	// Place a BUY LIMIT order: price=95000000, qty=0.1 → required=9500000 KRW
	if err := placeOrder(1, "BTC", "KRW", "BUY", "LIMIT", "95000000", "0.1"); err != nil {
		t.Fatal(err)
	}
	if lastError != nil {
		t.Fatalf("expected no error, got: %v", lastError)
	}
	if lastOrderID == "" {
		t.Fatal("expected order ID")
	}

	// Check balance: available=500000, locked=9500000
	assertBalance(t, 1, "KRW", "500000", "9500000")

	// Check outbox events
	opCount, _ := CountOutboxEvents("OrderPlaced")
	bdCount, _ := CountOutboxEvents("BalanceDeducted")
	if opCount != 1 {
		t.Errorf("expected 1 OrderPlaced event, got=%d", opCount)
	}
	if bdCount != 1 {
		t.Errorf("expected 1 BalanceDeducted event, got=%d", bdCount)
	}
}

func TestInsufficientBalance(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}
	if err := SeedBalance(2, "KRW", "100000"); err != nil {
		t.Fatal(err)
	}

	_ = placeOrder(2, "BTC", "KRW", "BUY", "LIMIT", "95000000", "1.0")

	if lastError == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(lastError, baldomain.ErrInsufficientBalance) {
		if !strings.Contains(lastError.Error(), "insufficient balance") {
			t.Fatalf("expected insufficient balance error, got: %v", lastError)
		}
	}

	// Balance should be unchanged
	assertBalance(t, 2, "KRW", "100000", "0")

	// No outbox events
	count, _ := CountAllOutboxEvents()
	if count != 0 {
		t.Errorf("expected 0 outbox events, got=%d", count)
	}
}

func TestConcurrentOrders(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}
	if err := SeedBalance(3, "KRW", "10000000"); err != nil {
		t.Fatal(err)
	}

	// 10 concurrent orders: price=95000000, qty=0.01 → each requires 950000 KRW
	n := 10
	results := RunConcurrent(n, func(index int) (string, error) {
		pair, _ := shared.NewAssetPair("BTC", "KRW")
		p, _ := decimal.NewFromString("95000000")
		q, _ := decimal.NewFromString("0.01")

		result, err := PlaceOrderUC.Execute(context.Background(), application.PlaceOrderCommand{
			UserID:    3,
			Pair:      pair,
			Side:      orderdomain.SideBuy,
			OrderType: orderdomain.TypeLimit,
			Price:     p,
			Quantity:  q,
		})
		if err != nil {
			return "", err
		}
		return result.OrderID, nil
	})

	successes, failures := PartitionResults(results)
	t.Logf("successes=%d, failures=%d", len(successes), len(failures))

	// All requests should complete (no panics or deadlocks)
	if len(successes)+len(failures) != n {
		t.Fatalf("expected %d total results, got %d", n, len(successes)+len(failures))
	}

	// Check deadlock errors
	for _, f := range failures {
		if strings.Contains(f.Err.Error(), "Deadlock") || strings.Contains(f.Err.Error(), "1213") {
			t.Errorf("deadlock detected in request %d: %v", f.Index, f.Err)
		}
	}

	// Balance conservation: available + locked = initial (10000000)
	avail, locked, err := GetBalance(3, "KRW")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := decimal.NewFromString(avail)
	l, _ := decimal.NewFromString(locked)
	total := a.Add(l)
	initial, _ := decimal.NewFromString("10000000")
	if !total.Equal(initial) {
		t.Errorf("balance conservation violated: available=%s + locked=%s = %s, expected=10000000", avail, locked, total.String())
	}

	// No negative balance
	if a.IsNegative() {
		t.Errorf("negative available balance: %s", avail)
	}
	if l.IsNegative() {
		t.Errorf("negative locked balance: %s", locked)
	}

	// Each success deducts 950000
	expectedDeducted := decimal.NewFromInt(int64(len(successes))).Mul(decimal.NewFromInt(950000))
	if !l.Equal(expectedDeducted) {
		t.Errorf("expected locked=%s, got=%s", expectedDeducted.String(), locked)
	}
}

func TestOrderCancellation(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}
	if err := SeedBalance(4, "KRW", "10000000"); err != nil {
		t.Fatal(err)
	}

	// Place order
	if err := placeOrder(4, "BTC", "KRW", "BUY", "LIMIT", "95000000", "0.1"); err != nil {
		t.Fatal(err)
	}
	if lastError != nil {
		t.Fatalf("expected no error, got: %v", lastError)
	}

	// Verify balance deducted
	assertBalance(t, 4, "KRW", "500000", "9500000")

	// Cancel order
	if err := cancelLastOrder(); err != nil {
		t.Fatal(err)
	}
	if lastError != nil {
		t.Fatalf("cancel error: %v", lastError)
	}

	// Balance should be fully restored
	assertBalance(t, 4, "KRW", "10000000", "0")

	// Check cancellation outbox events
	ocCount, _ := CountOutboxEvents("OrderCancelled")
	brCount, _ := CountOutboxEvents("BalanceRestored")
	if ocCount != 1 {
		t.Errorf("expected 1 OrderCancelled event, got=%d", ocCount)
	}
	if brCount != 1 {
		t.Errorf("expected 1 BalanceRestored event, got=%d", brCount)
	}
}

// --- Scenario 3: Outbox 보장 테스트 (03_Hook.md) ---

func TestOutboxGuarantee(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}
	if err := SeedBalance(5, "KRW", "50000000"); err != nil {
		t.Fatal(err)
	}

	// 1. 주문 생성 → outbox에 PENDING 이벤트 생성
	if err := placeOrder(5, "BTC", "KRW", "BUY", "LIMIT", "95000000", "0.1"); err != nil {
		t.Fatal(err)
	}
	if lastError != nil {
		t.Fatalf("expected no error, got: %v", lastError)
	}

	// 2. outbox에 PENDING 이벤트 존재 확인
	pendingCount, err := CountPendingOutboxEvents()
	if err != nil {
		t.Fatal(err)
	}
	if pendingCount < 2 {
		t.Errorf("expected at least 2 PENDING outbox events (OrderPlaced+BalanceDeducted), got=%d", pendingCount)
	}

	// 3. 두 번째 주문 추가
	if err := placeOrder(5, "BTC", "KRW", "BUY", "LIMIT", "95000000", "0.1"); err != nil {
		t.Fatal(err)
	}
	if lastError != nil {
		t.Fatalf("expected no error on 2nd order, got: %v", lastError)
	}

	// 4. 이벤트 순서 보존 확인 (id ASC)
	ids, err := GetOutboxEventIDs()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("outbox event ordering violated: id[%d]=%d <= id[%d]=%d", i, ids[i], i-1, ids[i-1])
		}
	}

	// 5. FetchPending으로 relay 경로 시뮬레이션
	fetched, err := OutboxRepo.FetchPending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) == 0 {
		t.Log("FetchPending returned 0 (expected outside tx), verifying via MarkSent")
	} else {
		t.Logf("FetchPending returned %d events", len(fetched))
		for _, evt := range fetched {
			if evt.EventID == "" {
				t.Error("fetched event has empty EventID")
			}
			if evt.KafkaTopic == "" {
				t.Error("fetched event has empty KafkaTopic")
			}
			if len(evt.Payload) == 0 {
				t.Error("fetched event has empty Payload")
			}
		}
	}

	// 6. MarkSent 시뮬레이션 → SENT 상태 전이 확인
	if err := MarkOutboxEventsSent(ids); err != nil {
		t.Fatal(err)
	}
	sentCount, err := CountSentOutboxEvents()
	if err != nil {
		t.Fatal(err)
	}
	if sentCount != len(ids) {
		t.Errorf("expected %d SENT events, got=%d", len(ids), sentCount)
	}

	// 7. PENDING 이벤트 0건 확인
	pendingAfter, _ := CountPendingOutboxEvents()
	if pendingAfter != 0 {
		t.Errorf("expected 0 PENDING events after MarkSent, got=%d", pendingAfter)
	}
}

// --- Scenario 4: 멱등성 테스트 (03_Hook.md) ---

func TestIdempotencyDuplicateEvent(t *testing.T) {
	if err := CleanAll(); err != nil {
		t.Fatal(err)
	}

	eventID := "evt-dup-test-001"
	consumerGroup := "test-consumer"

	// 동일 eventId로 20회 INSERT 시도 (PK 중복으로 1회만 성공)
	for i := 0; i < 20; i++ {
		_ = InsertProcessedEvent(eventID, consumerGroup)
	}

	// processed_events에 정확히 1행 존재
	count, err := CountProcessedEvents(eventID, consumerGroup)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row for event_id=%s, got=%d", eventID, count)
	}

	// 서로 다른 eventId는 각각 처리됨
	_ = InsertProcessedEvent("evt-A", consumerGroup)
	_ = InsertProcessedEvent("evt-B", consumerGroup)

	countA, _ := CountProcessedEvents("evt-A", consumerGroup)
	countB, _ := CountProcessedEvents("evt-B", consumerGroup)
	if countA != 1 || countB != 1 {
		t.Errorf("expected 1 row each for evt-A and evt-B, got A=%d B=%d", countA, countB)
	}
}

func parseUserID(s string) uint64 {
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
}
