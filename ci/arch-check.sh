#!/bin/bash
# ci/arch-check.sh - Architecture violation checker
# Based on 04_Fix.md checklist
set -euo pipefail

FAIL=0
WARN=0

echo "=========================================="
echo "  Architecture Violation Check"
echo "=========================================="

echo ""
echo "=== DDD-1: domain must not import infrastructure ==="
if grep -rn '".*infrastructure' internal/order/domain/ internal/balance/domain/ 2>/dev/null; then
    echo "FAIL: domain/ imports infrastructure/"
    FAIL=1
else
    echo "PASS"
fi

echo ""
echo "=== DDD-2: domain must not import database drivers ==="
if grep -rn '"database/sql"\|"github.com/jmoiron/sqlx"\|"github.com/go-sql-driver"' \
    internal/order/domain/ internal/balance/domain/ internal/shared/domain/ 2>/dev/null; then
    echo "FAIL: domain/ imports DB drivers"
    FAIL=1
else
    echo "PASS"
fi

echo ""
echo "=== MSA-1: no direct cross-BC imports in domain ==="
if grep -rn '"github.com/HongJungWan/harness-engineering/internal/balance' internal/order/domain/ 2>/dev/null; then
    echo "FAIL: order/domain/ directly imports balance/"
    FAIL=1
else
    echo "PASS"
fi
if grep -rn '"github.com/HongJungWan/harness-engineering/internal/order' internal/balance/domain/ 2>/dev/null; then
    echo "FAIL: balance/domain/ directly imports order/"
    FAIL=1
else
    echo "PASS"
fi

echo ""
echo "=== EDA-3: outbox insert must be in transaction context ==="
if grep -rn 'INSERT INTO outbox_events' internal/ 2>/dev/null | grep -v '_test.go' | grep -v 'tx\.\|Tx\.\|ExecContext' | head -5; then
    echo "WARNING: review outbox inserts for transaction context"
    WARN=$((WARN+1))
else
    echo "PASS"
fi

echo ""
echo "=== RELAY-1: relay must use SKIP LOCKED ==="
if grep -rn 'SKIP LOCKED' internal/outbox/ 2>/dev/null > /dev/null; then
    echo "PASS"
else
    echo "FAIL: relay worker missing SKIP LOCKED"
    FAIL=1
fi

echo ""
echo "=== PERF-5: no float64 for financial calculations in domain ==="
FLOAT_HITS=$(grep -rn 'float64' internal/order/domain/ internal/balance/domain/ 2>/dev/null | grep -v '_test.go' | grep -v '//' || true)
if [ -n "$FLOAT_HITS" ]; then
    echo "$FLOAT_HITS"
    echo "WARNING: float64 found in domain layer - verify it's not used for money"
    WARN=$((WARN+1))
else
    echo "PASS"
fi

echo ""
echo "=== DDD-4: repository interfaces in domain, implementations in infrastructure ==="
ORDER_REPO_IFACE=$(grep -rn 'type OrderRepository interface' internal/order/domain/ 2>/dev/null || true)
ORDER_REPO_IMPL=$(grep -rn 'type MysqlOrderRepository struct' internal/order/infrastructure/ 2>/dev/null || true)
if [ -n "$ORDER_REPO_IFACE" ] && [ -n "$ORDER_REPO_IMPL" ]; then
    echo "PASS (OrderRepository)"
else
    echo "FAIL: OrderRepository interface/impl not in correct packages"
    FAIL=1
fi

BALANCE_REPO_IFACE=$(grep -rn 'type BalanceRepository interface' internal/balance/domain/ 2>/dev/null || true)
BALANCE_REPO_IMPL=$(grep -rn 'type MysqlBalanceRepository struct' internal/balance/infrastructure/ 2>/dev/null || true)
if [ -n "$BALANCE_REPO_IFACE" ] && [ -n "$BALANCE_REPO_IMPL" ]; then
    echo "PASS (BalanceRepository)"
else
    echo "FAIL: BalanceRepository interface/impl not in correct packages"
    FAIL=1
fi

echo ""
echo "=== MSA-3: handlers must not expose domain entities directly ==="
if grep -rn 'json.NewEncoder.*order\.\|json.Marshal.*order\.' internal/order/presentation/ 2>/dev/null | grep -v 'Response\|dto\|DTO' | head -5; then
    echo "WARNING: handler may expose domain entities"
    WARN=$((WARN+1))
else
    echo "PASS"
fi

echo ""
echo "=== EDA-1: DLQ handling must exist in consumer ==="
if grep -rn 'DLQTopic\|DLQProducer\|publishToDLQ' internal/order/presentation/kafka_consumer.go 2>/dev/null > /dev/null; then
    echo "PASS"
else
    echo "FAIL: consumer missing DLQ handling"
    FAIL=1
fi

echo ""
echo "=== EDA-2: consumer idempotency via processed_events ==="
if grep -rn 'processed_events' internal/order/presentation/kafka_consumer.go 2>/dev/null > /dev/null; then
    echo "PASS"
else
    echo "FAIL: consumer missing processed_events idempotency check"
    FAIL=1
fi

echo ""
echo "=== RELAY-5: stuck event detection must exist ==="
if grep -rn 'CountStuckEvents\|detectStuckEvents\|stuck' internal/outbox/relay.go 2>/dev/null > /dev/null; then
    echo "PASS"
else
    echo "FAIL: relay missing stuck event detection"
    FAIL=1
fi

echo ""
echo "=== FACADE-1: relay.go must not import sarama directly ==="
if grep -rn '"github.com/IBM/sarama"' internal/outbox/relay.go 2>/dev/null; then
    echo "FAIL: relay.go directly imports sarama (should use EventProducer facade)"
    FAIL=1
else
    echo "PASS"
fi

echo ""
echo "=========================================="
echo "  Results: FAIL=$FAIL, WARN=$WARN"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    echo "Architecture checks FAILED. Fix violations before merging."
    exit 1
fi

echo "All architecture checks PASSED."
if [ $WARN -gt 0 ]; then
    echo "(with $WARN warnings - please review)"
fi
exit 0
