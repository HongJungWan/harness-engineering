# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Two layers live here:

1. **A Go service** — crypto exchange order + balance + outbox relay (`internal/`, `cmd/`, `test/`).
2. **A self-hosted Claude Code harness** — `docs/00_Design.md` … `docs/04_Fix.md` define executable contracts that drive long-running agent iterations; `.claude/harness/scripts/*.sh` are the Phase 2 runtime that actually executes those contracts.

**Before doing non-trivial work, read `docs/01_Plan.md`** — the agent's next task, DAG, and state schema live there. Design rationale for the Go code itself is in `docs/00_Design.md`.

## Build & Run Commands

```bash
# Go build / run
make build                    # Output: bin/harness-order
make run                      # requires docker-up first

# Infrastructure (docker-compose: MySQL 8.0 + Kafka KRaft)
make docker-up                # Start containers; applies scripts/init.sql on first run
make docker-down              # Stop
make docker-debug             # Also starts Kafka UI on :8989 (profile: debug)
make docker-clean             # Stop + remove volumes (wipes DB data)

# Tests (integration — MySQL + Kafka must be up)
make test                     # -race, all
make test-short               # -short, skips integration
go test -v -run TestOrderPlacement_HappyPath ./test/acceptance/support/...

# Lint / arch (legacy CI entry — Phase 2's check.sh supersedes for the harness loop)
make lint
make arch-check               # ci/arch-check.sh — 12 DDD/MSA/EDA/RELAY/FACADE rules

# Kafka inspection
make kafka-topics
make kafka-consume            # Tail order.events

# Harness (Phase 2)
.claude/harness/scripts/validate-plan.sh          # Plan.md schema check
.claude/harness/scripts/next-task.sh              # Pick current task, emit <harness-context>
.claude/harness/scripts/check.sh --all            # Full arch check (HARNESS_ grammar)
.claude/harness/scripts/check.sh <file>           # Check rules applicable to that file
.claude/harness/scripts/check.sh --only DDD-1     # Single rule

# Note: Makefile has migrate-up/down/status stubs, but there is no ./migrations/ dir.
# Schema lives in scripts/init.sql and is applied by docker-compose on first-run init.
```

## Harness (the self-hosted execution environment)

The 5 docs under `docs/` are not prose; they are **machine contracts** that hooks and scripts consume.

| doc | role | read at |
|---|---|---|
| `00_Design.md` | Narrative: DDD/EDA/load design, BDD scenarios. Why the Go code looks the way it does. | Once, for background |
| `01_Plan.md` | Task DAG (yaml fenced block) + `state.json` JSON schema + transition rules + next-task algorithm. Read-only. | Every UserPromptSubmit (`next-task.sh`) |
| `02_Code.md` | Pattern registry (forbidden/required regex + glob + severity) + file templates + dep whitelist | Before writing code; every PostToolUse (`check.sh`) |
| `03_Hook.md` | Hook inventory + `HARNESS_*` stdout grammar + `.claude/settings.json` wiring | Setup + on hook failure |
| `04_Fix.md` | `reason_key` → fix recipe lookup (16 rows) + N=3 escalation rules | After any `HARNESS_HOOK_FAIL` |
| `05_Review.md` | **Phase 2 target**: to be redesigned as a review-bot hook (currently legacy audit report) | — |

Each doc ends with a **"실행 아티팩트 매핑"** section enumerating the scripts/files that materialize it under `.claude/harness/`. Phase 2 implementation should be transcription, not redesign.

### Runtime layout

```
.claude/
├── settings.json                      # Hook wiring (4 hooks → 4 scripts)
├── settings.local.json                # user's local overrides (do not touch)
└── harness/
    ├── state.json                     # current task + per-task state
    ├── last-failure.json              # most recent HARNESS_HOOK_FAIL (transient, gitignored)
    ├── lib/
    │   ├── common.sh                  # shared bash helpers (sourced)
    │   ├── plan-to-json.py            # yaml fence in Plan.md → JSON
    │   └── dfs-cycle.py               # DAG cycle detection for validate-plan
    ├── scripts/
    │   ├── next-task.sh               # UserPromptSubmit — pick & inject current task
    │   ├── validate-plan.sh           # PreToolUse on docs/01_Plan.md — DAG invariants
    │   ├── check.sh                   # PostToolUse — run applicable rules
    │   └── commit-and-advance.sh      # Stop — state transition + failure streak + escalation (커밋은 안 함)
    ├── logs/hook-<date>.log           # append-only stderr sink (gitignored)
    └── blocked/<task_id>.md           # escalation dumps (gitignored)
```

### Loop (one iteration)

1. `UserPromptSubmit` → `next-task.sh` emits `<harness-context>` with current task + exit_criteria + last failure recipe hint
2. Agent edits files (`Write` / `Edit`)
3. `PostToolUse` → `check.sh <file>` runs applicable rules; first FAIL lands in `last-failure.json`
4. On failure the next turn's `next-task.sh` injects the recipe hint from `04_Fix.md`; agent applies, retries
5. `Stop` → `commit-and-advance.sh` runs full `check.sh`; all pass → state transition `in_progress → done` (커밋은 사용자가 직접 수행)
6. Same `reason` 3× consecutively → task `blocked` + `.claude/harness/blocked/<id>.md` 덤프, human unblocks

### Tool dependencies

Phase 2 scripts require: `python3` (with PyYAML), `jq`, standard POSIX tools. Preflight in `lib/common.sh` exits with `HARNESS_INFRA_ERROR` if missing.

### Git convention

커밋은 하네스가 하지 않음 — **사용자가 직접 수행**. 하네스는 state/rules/checks/remediation 만 담당.
커밋 시 `docs/01_Plan.md §6` 의 task 별 `commit_subject` / `commit_body` 를 참고하면 일관된 메시지 유지 가능.

## Architecture

Go 1.25 MSA: crypto-exchange order service with Transactional Outbox for reliable Kafka delivery. Deep rationale in `docs/00_Design.md`.

### Repo Layout (non-obvious entry points)

- `cmd/server/main.go` — DI wiring; constructs repos/usecases, spawns N relay workers as goroutines, graceful shutdown
- `internal/{order,balance,outbox,shared}/` — bounded contexts with `domain/ application/ infrastructure/ presentation/` layers. Not every BC has all four: Balance has no application layer (Order's use cases orchestrate Balance), Outbox is infra-only
- `scripts/init.sql` — canonical schema (5 tables: `orders`, `balances`, `outbox_events`, `processed_events`, `idempotency_keys`)
- `test/acceptance/{features,support}/` — `.feature` BDD specs next to Go test runners
- `ci/arch-check.sh` — legacy 12-rule static check; Phase 2's `check.sh` supersedes for the harness loop but keeps the same rule IDs (DDD-1…FACADE-1)

### Bounded Contexts (same MySQL instance, code-separated)

- **Order BC** (`internal/order/`) — lifecycle (PENDING → ACCEPTED → FILLED/CANCELLED)
- **Balance BC** (`internal/balance/`) — user asset ledger (deduct / lock / unlock / settle)
- **Outbox** (`internal/outbox/`) — relay worker: polls PENDING → Kafka
- **Shared** (`internal/shared/`) — `TxManager`, `EventProducer` facade, VOs (Money, AssetPair)

### Layer Rules (enforced by `ci/arch-check.sh` and Phase 2 `check.sh`)

```
[presentation] → [application] → [domain] ← [infrastructure]
```

- `domain/` must never import `infrastructure/`, `database/sql`, or `sqlx` (DDD-1, DDD-2)
- `order/domain/` must never import `balance/` (MSA-1) — cross-BC coordination lives in `application/` only
- Handlers return DTOs, never domain entities (MSA-3)
- `outbox/relay.go` uses the `EventProducer` interface, never imports `sarama` directly (FACADE-1)

### Core Transaction Flow (`PlaceOrderUseCase`)

Lock order is always **balance first, then order** to prevent deadlocks:

1. Check idempotency key
2. Create Order + compute required amount
3. `BEGIN TX`
4. `SELECT ... FROM balances FOR UPDATE` (pessimistic lock)
5. `balance.DeductAndLock(amount)`
6. `order.Accept()` (emits OrderPlaced event)
7. `balanceRepo.Save()` → UPDATE balances + INSERT outbox_events
8. `orderRepo.Save()` → INSERT orders + INSERT outbox_events
9. INSERT idempotency_keys
10. `COMMIT`

### Outbox Relay Worker

- Polls with `FOR UPDATE SKIP LOCKED` (multi-worker safe)
- Kafka `acks=all` + `idempotent=true` before marking `SENT`
- Exponential backoff on retry: `base * 2^retryCount`
- Stuck event detection every 60s (`PENDING > 5 minutes`)
- Max retries → `FAILED`

### Key Conventions

- All financial math uses `shopspring/decimal` — `float64` forbidden in domain (PERF-5)
- Domain events are created inside Aggregate Root methods, drained via `PullEvents()`
- DB transactions via context-embedded `TxManager.RunInTx()` — tx rides in context
- Sentinel errors defined in each domain's `errors.go`
- **Event drainage**: repository `Save()` is responsible for calling `PullEvents()` and inserting `outbox_events` rows *in the same sqlx.Tx*. Never publish to Kafka from the aggregate directly — the relay owns that (EDA-3)
- **EventProducer facade**: interface in `internal/shared/domain/producer.go`, Sarama impl in `internal/shared/infrastructure/sarama_producer.go`. `internal/outbox/relay.go` must depend on the interface only (FACADE-1)
- **Consumer idempotency**: `internal/order/presentation/kafka_consumer.go` wraps the handler with DLQ (`order.events.dlq`) + `processed_events` dedup in a single tx — check → process → mark (EDA-1, EDA-2)

## Configuration

All config via environment variables — full list in `.env.example`, parsed with `caarlos0/env` under prefixes `APP_`, `DB_`, `KAFKA_`, `RELAY_`. Validated in `internal/config/config.go` (e.g. `MaxIdleConns ≤ MaxOpenConns`, `RelayBatchSize ∈ [1, 1000]`). Key defaults:

- `APP_PORT=8080`, `DB_PORT=3306`, `KAFKA_BROKERS=localhost:9092`
- Pool: `MaxOpenConns=50`, `MaxIdleConns=25`
- Relay: 2 workers, 50ms poll interval, batch 100, max 5 retries
- Topics: `order.events` (main), `order.events.dlq` (DLQ), consumer group `order-processor`

## Testing

Integration tests against real MySQL + Kafka (started via `make docker-up`). No mocks. Six acceptance scenarios under `test/acceptance/`:

- Happy path, insufficient balance, concurrent orders (10-goroutine barrier), order cancellation, outbox guarantee, idempotency deduplication
- `test/acceptance/features/*.feature` — BDD specs; execution lives in Go `Test*` funcs under `test/acceptance/support/`
- `support/suite_test.go` owns `TestMain` (wires repos once), plus `CleanAll()` + `SeedBalance()` for per-test isolation
- Concurrent scenarios use `RunConcurrent()` in `support/concurrent.go` (channel-based barrier)
