# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Build
make build                    # Output: bin/harness-order

# Run (requires MySQL + Kafka running)
make run

# Infrastructure
make infra-up                 # Start MySQL 8.0 + Kafka (KRaft)
make infra-down               # Stop all containers
make infra-debug              # Includes Kafka UI on :8989

# Tests
make test                     # All tests with -race (needs running MySQL + Kafka)
go test -v -run TestOrderPlacement_HappyPath ./test/acceptance/support/...  # Single test

# Lint
make lint                     # golangci-lint

# Architecture validation
make arch-check               # Runs ci/arch-check.sh — verifies DDD/MSA/EDA rules

# Database
make migrate-up               # Apply migrations
make migrate-down             # Rollback 1 migration

# Kafka inspection
make kafka-topics             # List topics
make kafka-consume-orders     # Tail order.events topic
```

## Architecture

Go 1.25 MSA project: a crypto exchange order service with Transactional Outbox pattern for reliable Kafka event delivery.

### Bounded Contexts (same MySQL instance, code-separated)

- **Order BC** (`internal/order/`) — order lifecycle (PENDING → ACCEPTED → FILLED/CANCELLED)
- **Balance BC** (`internal/balance/`) — user asset management (deduct/lock/unlock/settle)
- **Outbox** (`internal/outbox/`) — relay worker that polls PENDING events and publishes to Kafka
- **Shared** (`internal/shared/`) — TxManager, EventProducer facade, Value Objects (Money, AssetPair)

### Layer Rules (enforced by `ci/arch-check.sh`)

```
[presentation] → [application] → [domain] ← [infrastructure]
```

- `domain/` must never import `infrastructure/`, `database/sql`, or `sqlx`
- `order/domain/` must never import `balance/` (cross-BC coupling forbidden)
- Cross-BC coordination happens only in `application/` layer via interfaces
- Handlers return DTOs, never domain entities
- Outbox relay uses `EventProducer` interface, never imports `sarama` directly

### Core Transaction Flow (PlaceOrderUseCase)

Lock ordering is always **balance first, then order** to prevent deadlocks:

1. Check idempotency key (cache hit → return stored response)
2. Create Order + calculate required amount
3. `BEGIN TX`
4. `SELECT ... FROM balances FOR UPDATE` (pessimistic lock)
5. `balance.DeductAndLock(amount)`
6. `order.Accept()` (emits OrderPlaced event)
7. `balanceRepo.Save()` → UPDATE balances + INSERT outbox_events
8. `orderRepo.Save()` → INSERT orders + INSERT outbox_events
9. INSERT idempotency_keys
10. `COMMIT`

### Outbox Relay Worker

- Polls with `FOR UPDATE SKIP LOCKED` (multiple workers safe)
- Kafka `acks=all` + `idempotent=true` before marking SENT
- Exponential backoff on retry: `base * 2^retryCount`
- Stuck event detection every 60s (PENDING > 5 minutes)
- Max retries → FAILED status

### Key Conventions

- All financial math uses `shopspring/decimal` — `float64` is forbidden in domain
- Domain events are created inside Aggregate Root methods, drained via `PullEvents()`
- DB transactions use context-based `TxManager.RunInTx()` — tx is embedded in context
- Consumer idempotency via `processed_events` table (single tx: check → process → mark)
- Sentinel errors defined in each domain's `errors.go`

## Configuration

All config via environment variables (see `.env.example`). Key defaults:
- `APP_PORT=8080`, `DB_PORT=3306`, `KAFKA_BROKERS=localhost:9092`
- Connection pool: `MaxOpenConns=50`, `MaxIdleConns=25`
- Relay: 2 workers, 50ms poll interval, batch size 100, max 5 retries

## Testing

Tests are integration tests against real MySQL and Kafka (started via `make infra-up`).
No mocks — uses `docker-compose` infrastructure. Six acceptance test scenarios in `test/acceptance/`:

- Happy path, insufficient balance, concurrent orders (10 goroutines with barrier pattern), order cancellation, outbox guarantee, idempotency deduplication
