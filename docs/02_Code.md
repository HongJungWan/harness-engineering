# 2단계: Code (Pattern Registry + File Templates)

> **역할**: 에이전트가 코드를 **작성하기 직전** 과 **PostToolUse hook 검증** 시 참조하는 계약.
> - 작성 전: 해당 파일 유형의 템플릿 (§3) 을 뼈대로 사용.
> - 작성 후: Pattern Registry (§2) 의 모든 FAIL 항목이 clean 이어야 done.
>
> 설계 배경(왜 이 규칙이 있는가)은 `00_Design.md §5`.

---

## 1. 디렉토리 구조 규칙

```
harness-engineering/
├── cmd/server/main.go                         # DI wiring only. 비즈니스 로직 금지
├── internal/
│   ├── config/config.go                       # env 파싱. 도메인 import 금지
│   ├── {order,balance}/
│   │   ├── domain/                            # 순수 도메인. infra import 금지
│   │   ├── application/                       # usecase. 트랜잭션 오케스트레이션
│   │   ├── infrastructure/                    # MySQL/Kafka 구현
│   │   └── presentation/                      # HTTP/Kafka 엔드포인트
│   ├── shared/{domain,infrastructure}/        # 공유 VO/인터페이스/구현
│   └── outbox/                                # infra-only (relay worker)
├── scripts/init.sql                           # DDL (schema canonical)
├── ci/arch-check.sh                           # 레거시: Phase 2 에서 check.sh 로 흡수
└── test/acceptance/{features,support}/        # BDD
```

**강제 규칙 (glob → 금지 내용):**

| glob | 금지 import | reason_key |
|---|---|---|
| `internal/*/domain/**/*.go` | `github.com/HongJungWan/harness-engineering/internal/*/infrastructure` | ddd-1 |
| `internal/*/domain/**/*.go` | `database/sql`, `github.com/jmoiron/sqlx`, `github.com/go-sql-driver/mysql` | ddd-2 |
| `internal/order/domain/**/*.go` | `internal/balance/**` | msa-1 |
| `internal/balance/domain/**/*.go` | `internal/order/**` | msa-1 |
| `internal/outbox/relay.go` | `github.com/IBM/sarama` | facade-1 |

---

## 2. Pattern Registry

각 행은 Phase 2 의 `check.sh` 가 정확히 이 명령으로 검사한다. **`id` 는 03_Hook.md 의 출력·04_Fix.md 의 recipe key 와 완전히 매칭**되어야 한다 (UPPERCASE = 규칙 ID, lowercase-dash = reason_key).

### 2.1 Forbidden patterns (매치되면 FAIL)

| id / reason_key | kind | applies_to (glob) | 검사 명령 (bash) | severity |
|---|---|---|---|---|
| `DDD-1` / `ddd-1` | forbidden-import | `internal/order/domain/ internal/balance/domain/ internal/shared/domain/` | `grep -rn '".*infrastructure' $GLOB` | FAIL |
| `DDD-2` / `ddd-2` | forbidden-import | 위 동일 | `grep -rnE '"(database/sql\|github.com/jmoiron/sqlx\|github.com/go-sql-driver)"' $GLOB` | FAIL |
| `MSA-1` / `msa-1` | forbidden-import | `internal/order/` (except `application/`, `*_test.go`) | `grep -rn '"github.com/HongJungWan/harness-engineering/internal/balance' $GLOB` | FAIL |
| `FACADE-1` / `facade-1` | forbidden-import | `internal/outbox/relay.go` | `grep -n '"github.com/IBM/sarama"' internal/outbox/relay.go` | FAIL |
| `PERF-5` / `perf-5` | forbidden-type | `internal/*/domain/**/*.go` (non-test) | `grep -rnE '\bfloat(32\|64)\b' $GLOB \| grep -v _test.go \| grep -vE '^\s*//'` | FAIL |
| `MSA-3` / `msa-3` | forbidden-pattern | `internal/*/presentation/handler.go` | `grep -nE 'json\.Marshal\([^)]*domain\.' $GLOB` | FAIL |

### 2.2 Required patterns (없으면 FAIL)

| id / reason_key | kind | applies_to | 검사 명령 | severity |
|---|---|---|---|---|
| `DDD-4` / `ddd-4` | required-structure | `internal/{order,balance}/` | 각 BC 에 `domain/repository.go` 에 `type *Repository interface` 가 있고, `infrastructure/mysql_*_repo.go` 에 `type Mysql*Repository struct` 가 있어야 함 | FAIL |
| `EDA-1` / `eda-1` | required-token | `internal/order/presentation/kafka_consumer.go` | `grep -qE '(DLQTopic\|DLQProducer\|publishToDLQ)' $FILE` | FAIL |
| `EDA-2` / `eda-2` | required-token | `internal/order/presentation/kafka_consumer.go` | `grep -q 'processed_events' $FILE` | FAIL |
| `EDA-3` / `eda-3` | required-pattern | `internal/*/infrastructure/mysql_*_repo.go` | 모든 `INSERT INTO outbox_events` 주변 N줄에 `tx.ExecContext` 또는 `Tx.Exec` 이 존재 | FAIL |
| `RELAY-1` / `relay-1` | required-token | `internal/outbox/` | `grep -rn 'SKIP LOCKED' internal/outbox/` | FAIL |
| `RELAY-5` / `relay-5` | required-token | `internal/outbox/relay.go` | `grep -nE '(CountStuckEvents\|detectStuckEvents\|stuck)' $FILE` | FAIL |

### 2.3 Build / Test patterns

| id / reason_key | kind | 검사 명령 | severity |
|---|---|---|---|
| `GO-BUILD` / `go-build` | build | `go build ./... 2>&1` | FAIL (exit≠0) |
| `GO-TEST` / `go-test` | test | `go test -race -count=1 $PACKAGES 2>&1` | FAIL (exit≠0) |
| `GO-LINT` / `go-lint` | lint | `golangci-lint run ./...` | FAIL (exit≠0) |
| `GO-VET` / `go-vet` | vet | `go vet ./...` | WARN |

### 2.4 Pattern Registry 직렬화

위 표는 `.claude/harness/patterns.tsv` 에 아래 TSV 로 materialize (Phase 2). tab-separated, 헤더 한 줄:

```
id	kind	glob	cmd	severity
DDD-1	forbidden-import	internal/*/domain/	grep -rn '".*infrastructure' $GLOB	FAIL
DDD-2	forbidden-import	internal/*/domain/	grep -rnE '"(database/sql|github.com/jmoiron/sqlx|github.com/go-sql-driver)"' $GLOB	FAIL
...
```

`check.sh` 가 이 TSV 를 읽어 각 행의 `cmd` 를 실행, exit code 로 PASS/FAIL 판정.

---

## 3. File Templates

Phase 2 는 아래 스켈레톤을 `.claude/harness/templates/*.tmpl` 로 저장. task `files.creates` 의 경로 패턴에 매칭되는 템플릿을 에이전트가 시작점으로 사용.

### 3.1 Aggregate Root (`internal/{bc}/domain/{entity}.go`)

```go
// Package domain — pure business logic for the {BC} bounded context.
// MUST NOT import infrastructure, database/sql, sqlx, or other BCs.
package domain

import (
    "time"

    shareddomain "github.com/HongJungWan/harness-engineering/internal/shared/domain"
    "github.com/shopspring/decimal"
)

type {Entity} struct {
    // ... fields using shopspring/decimal, NOT float64
    events []shareddomain.DomainEvent
}

// Business methods mutate state AND append domain events:
//   e.g. o.events = append(o.events, {Event}{...})

func (e *{Entity}) PullEvents() []shareddomain.DomainEvent {
    evts := e.events
    e.events = nil
    return evts
}
```

### 3.2 Repository Interface (`internal/{bc}/domain/repository.go`)

```go
package domain

import "context"

// Defined in domain/ (DDD-4). Impl MUST live in infrastructure/.
type {Entity}Repository interface {
    Save(ctx context.Context, e *{Entity}) error
    FindByID(ctx context.Context, id string) (*{Entity}, error)
    // pessimistic-lock variant if needed:
    FindByUserAndCurrencyForUpdate(ctx context.Context, userID int64, cur string) (*{Entity}, error)
}
```

### 3.3 MySQL Repository (`internal/{bc}/infrastructure/mysql_{entity}_repo.go`)

```go
package infrastructure

import (
    "context"

    domain "github.com/HongJungWan/harness-engineering/internal/{bc}/domain"
    "github.com/HongJungWan/harness-engineering/internal/outbox"
    sharedinfra "github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

type Mysql{Entity}Repository struct {
    outbox outbox.OutboxRepository   // EDA-3: Save() must drain events into outbox in same Tx
}

// Save MUST:
//  1. Pull tx from context (embedded by TxManager)
//  2. UPSERT the aggregate row
//  3. Call entity.PullEvents() → insert each into outbox_events via the SAME tx
// If (3) is skipped or uses a different tx connection, EDA-3 will FAIL.
func (r *Mysql{Entity}Repository) Save(ctx context.Context, e *domain.{Entity}) error {
    tx := sharedinfra.TxFromContext(ctx)
    // ... UPDATE/INSERT entity via tx.ExecContext(...)
    for _, evt := range e.PullEvents() {
        if err := r.outbox.InsertEvent(ctx, evt); err != nil { // uses same tx via ctx
            return err
        }
    }
    return nil
}
```

### 3.4 UseCase (`internal/{bc}/application/{verb}.go`)

```go
package application

// Lock order: balance first, then order. See 00_Design.md §4.5.
func (uc *PlaceOrderUseCase) Execute(ctx context.Context, cmd PlaceOrderCommand) (*PlaceOrderResult, error) {
    // 1. idempotency check
    // 2. construct VOs + compute required amount
    // 3. tx := TxManager.RunInTx(ctx, func(ctx) error {
    //      4. bal := balanceRepo.FindByUserAndCurrencyForUpdate(ctx, ...)   // SELECT FOR UPDATE
    //      5. bal.DeductAndLock(amount)                                       // domain method
    //      6. order := NewOrder(...).Accept()                                 // domain method
    //      7. balanceRepo.Save(ctx, bal)    // UPDATE balances + INSERT outbox_events
    //      8. orderRepo.Save(ctx, order)    // INSERT orders    + INSERT outbox_events
    //      9. idemRepo.Put(ctx, key, response)
    //    })
    // 10. return response
}
```

### 3.5 Kafka Consumer (`internal/order/presentation/kafka_consumer.go`)

```go
package presentation

// Implements sarama.ConsumerGroupHandler. Required contracts:
//  - EDA-1: DLQ path must exist (DLQTopic / publishToDLQ).
//  - EDA-2: processed_events dedup inside a single tx (check → process → mark).
type IdempotentConsumer struct {
    dlqTopic string
    // ...
}

func (c *IdempotentConsumer) ConsumeClaim(...) error {
    // TxManager.RunInTx:
    //   if processedEventsRepo.Exists(tx, eventID, consumerGroup) { return nil }  // dedup
    //   if err := handler(ctx, msg); err != nil {
    //       publishToDLQ(ctx, msg, err)  // EDA-1
    //       return nil                    // commit offset so we don't loop
    //   }
    //   processedEventsRepo.Insert(tx, eventID, consumerGroup)
}
```

### 3.6 HTTP Handler (`internal/{bc}/presentation/handler.go`)

```go
package presentation

// MSA-3: return DTOs only. Never marshal domain entities.
func (h *Handler) Place(w http.ResponseWriter, r *http.Request) {
    var req PlaceOrderRequest
    // ... decode, call usecase ...
    resp := PlaceOrderResponse{ /* map from domain → DTO */ }
    json.NewEncoder(w).Encode(resp)  // DTO, NOT domain.Order
}
```

---

## 4. Go 의존성 화이트리스트

도메인 계층은 아래 외부 패키지 외에 어떤 것도 import 금지. `check.sh` 의 DDD-2 가 강제.

```
github.com/shopspring/decimal       # 금융 연산
github.com/google/uuid              # UUIDv7
```

`infrastructure/`, `presentation/` 계층은 추가 허용:
```
github.com/jmoiron/sqlx
github.com/go-sql-driver/mysql
github.com/IBM/sarama               # 단, internal/outbox/relay.go 는 금지 (facade-1)
github.com/go-chi/chi/v5
github.com/caarlos0/env/v10
```

---

## 5. 코딩 컨벤션 (grep 으로 강제 가능한 것만)

| 규칙 | 강제 방법 |
|---|---|
| 금융 연산 `float64` 금지 | `perf-5` 패턴 |
| 도메인 이벤트는 Aggregate Root 내부에서만 생성 | `grep -rn 'OrderPlaced{' internal/` 가 domain/ 외부에서 매치되면 FAIL (향후 추가 규칙 `DDD-5`) |
| Context propagation | `grep -rnE 'func \([^)]*\) [A-Z][a-zA-Z]*\(' internal/*/infrastructure/*.go \| grep -v 'context.Context'` 가 비어 있어야 함 (향후 규칙 `PERF-3`) |
| sentinel error 는 domain/errors.go | `grep -rn 'var Err[A-Z]' internal/*/` 결과가 전부 `domain/errors.go` 안이어야 함 |

**grep 으로 강제 불가능한 규칙** (코드 리뷰에서만 잡힘):
- "Tell, Don't Ask"
- Aggregate boundary 적절성
- 이벤트 payload 자기 서술성

→ 이런 것들은 Phase 2 에서 `semgrep` / `ast-grep` 으로 upgrade 가능. Phase 1 은 grep 으로 표현 가능한 것만 계약에 올림.

---

## 6. 실행 아티팩트 매핑 (Phase 2 구현 대상)

| 아티팩트 | 경로 | 입력 | 출력 / 부작용 |
|---|---|---|---|
| Pattern registry | `.claude/harness/patterns.tsv` | — | §2 TSV. `check.sh` 의 입력 |
| File templates | `.claude/harness/templates/{aggregate,repo-iface,mysql-repo,usecase,consumer,handler}.tmpl` | — | `next-task.sh` 가 task.files.creates 매칭해 에이전트에 stdout 으로 노출 |
| `check.sh` | `.claude/harness/scripts/check.sh <file...>` | 변경된 파일 목록 (또는 `--all`) | patterns.tsv 의 해당 glob 매치 행만 실행. stdout: `HARNESS_HOOK_PASS name=<id>` 또는 `HARNESS_HOOK_FAIL name=<id> reason=<reason-key> file=<p:l>`. exit 0 pass, 1 fail, 2 infra |
| `arch-check.sh` (레거시) | `ci/arch-check.sh` | — | Phase 2 에서 `check.sh --all --only-arch` 로 축소·호출 wrapper 화 |
| Template lookup | `.claude/harness/scripts/template-for.sh <path>` | 파일 경로 | stdout: 매칭 템플릿 경로 (없으면 exit 1) |

**Phase 2 검증:**
- patterns.tsv 의 모든 `id` 가 §2 표와 1:1.
- patterns.tsv 의 모든 `id` 가 `04_Fix.md` 의 recipe 테이블에 대응하는 `reason-key` 를 가짐.
- `check.sh --dry-run` 이 patterns.tsv 를 파싱만 하고 각 행의 glob 이 유효한지 검증.
