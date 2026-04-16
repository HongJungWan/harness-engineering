# 1단계: Plan (Task DAG + State Machine 계약)

> **역할**: 하네스가 자율 실행되는 동안 "다음에 뭘 할지"를 결정하는 **읽기 전용** 계약.
> 에이전트는 매 iteration 시작 시 이 문서의 `tasks:` 블록과 `.claude/harness/state.json`을 읽고,
> §4의 next-task 알고리즘을 **결정론적으로** 돌려 다음 task 를 고른다.
>
> 설계 배경(왜 이 task 들이 필요한가)은 `00_Design.md` 에 있다. 이 파일은 **실행 스펙**이다.

---

## 1. Task 스키마

각 task 는 아래 스키마를 만족해야 한다 (`validate-plan.sh` 가 검증):

```yaml
tasks:
  - id: <STRING>              # 고유. BC 접두어 + 번호. 예: ORDER-001, RELAY-003
    title: <STRING>           # 사람용 한 줄
    deps: [<id>, ...]         # 선행 task id 목록 (acyclic)
    files:
      creates: [<path>, ...]  # 이 task 로 새로 생성될 파일 (optional)
      modifies: [<path>, ...] # 기존 파일 수정 (optional)
    exit_criteria:            # 모두 통과해야 done 으로 전이
      - hook: arch-check
        rules: [<RULE-ID>, ...]   # 02_Code.md의 pattern id (DDD-1, EDA-3, ...)
      - hook: go-build          # 선택: 지정 패키지만 빌드
        packages: [<pkg>, ...]
      - hook: go-test           # 선택: 지정 테스트만
        tests: [<TestName>, ...]
    blocking: false            # true 면 전이 실패 시 전체 큐 중단 (default false)
```

**제약:**
- `id` 는 전 프로젝트에서 유일.
- `deps` 는 DAG 여야 함 (순환 금지).
- 모든 `rules` 는 `02_Code.md` 의 패턴 레지스트리에 존재해야 함.
- 모든 `hook` 이름은 `03_Hook.md` 의 hook 인벤토리에 존재해야 함.

---

## 2. state.json 스키마

경로: `.claude/harness/state.json` (git 추적, task 커밋과 원자적으로 갱신)

```json
{
  "version": 1,
  "plan_sha256": "<Plan.md 의 tasks: 블록 SHA256>",
  "current_task": "ORDER-003",
  "tasks": {
    "ORDER-001": {
      "state": "done",
      "attempts": 1,
      "commit_sha": "a1b2c3d...",
      "completed_at": "2026-04-16T10:00:00Z",
      "stash_ref": null
    },
    "ORDER-003": {
      "state": "in_progress",
      "attempts": 2,
      "failure_streak": { "reason": "ddd-1", "count": 1 },
      "last_failure_at": "2026-04-16T10:25:00Z"
    }
  },
  "updated_at": "2026-04-16T10:25:00Z"
}
```

**상태 값** (enum):

| 값 | 의미 |
|---|---|
| `pending` | 아직 시작 안함. deps 가 모두 `done` 이 되면 선택 가능 |
| `in_progress` | 에이전트가 현재 작업 중. `current_task` 와 일치해야 함 |
| `done` | exit_criteria 전부 통과, 커밋됨 |
| `blocked` | escalation 으로 중단. 사람 개입 필요 |

---

## 3. 상태 전이 규칙

전이는 오직 `.claude/harness/scripts/commit-and-advance.sh` (성공) 또는 `escalate.sh` (실패) 만 쓴다. **에이전트는 state.json 을 직접 수정하지 않는다** — 상태 기계의 결정론을 보장하기 위함.

```
pending ──(next-task 선택 시)──> in_progress
  조건: deps 전부 done, current_task 가 비어있음

in_progress ──(exit_criteria 전부 PASS)──> done
  조건: 모든 hook 이 HARNESS_HOOK_PASS 또는 종료 코드 0
  부작용: git commit, current_task ← null, state.json 갱신

in_progress ──(같은 reason 으로 N=3회 연속 실패)──> blocked
  부작용: HARNESS_ESCALATION 출력, current_task ← null, 다음 unblocked task 선택

blocked ──(사람이 수동 unblock)──> pending | in_progress
  트리거: 사람이 `scripts/unblock.sh <task-id>` 실행 후 state 수동 조정

in_progress ──(WIP 중단: 사용자 요청)──> pending
  부작용: git stash → stash_ref 기록
```

**불변식:**
- `current_task` 가 비어있지 않으면 해당 task state 는 반드시 `in_progress`.
- `failure_streak.count >= 3` 이면 해당 task state 는 `blocked`.
- `done` 인 task 는 `commit_sha` 필수.

---

## 4. Next-Task 선택 알고리즘

모든 구현은 아래 pseudocode 와 **결정론적으로** 동치여야 한다.

```python
def next_task(plan, state):
    if state.current_task is not None:
        return state.current_task              # 재개

    candidates = [
        t for t in plan.tasks
        if state.tasks[t.id].state == "pending"
        and all(state.tasks[d].state == "done" for d in t.deps)
    ]
    if not candidates:
        if any(t.state == "blocked" for t in state.tasks.values()):
            return None                         # HALT: 사람 개입 대기
        if all(t.state == "done" for t in state.tasks.values()):
            return None                         # DONE: 전체 완료
        raise DeadlockError()                   # 불가능: dep 그래프 버그

    # Plan.md 의 tasks: 순서 그대로 (topological 순서 보존)
    return min(candidates, key=lambda t: plan.index(t.id))
```

**전역 종료 조건:**
- 모든 task `done` → "완료" 메시지.
- 남은 pending 이 있는데 고를 수 없음 + blocked 존재 → HALT, escalation.
- 남은 pending 이 있는데 고를 수 없음 + blocked 없음 → DAG 버그 → `validate-plan.sh` 재실행.

---

## 5. Git 커밋 컨벤션 (state = git log 에서 재구성 가능)

모든 task 완료 커밋은 아래 형식을 지킨다. `commit-and-advance.sh` 가 생성.

```
<type>(<task_id>): <subject>

<optional body>

task_id: <ID>
state: <from> → <to>
hooks_passed: <csv of hook names>
attempts: <N>
```

`<type>` ∈ {`feat`, `fix`, `refactor`, `test`, `chore`, `docs`}.

**예시:**
```
feat(ORDER-001): Add Order aggregate with state machine

Implements PENDING→ACCEPTED→FILLED/CANCELLED transitions and
OrderPlaced / OrderCancelled domain events.

task_id: ORDER-001
state: in_progress → done
hooks_passed: arch-check, go-build, go-test
attempts: 1
```

**state.json 재구성:** `git log --pretty=%B | awk` 로 trailer 추출 → 각 task 의 최신 상태를 결정. state.json 이 손상되어도 복구 가능.

---

## 6. 부트스트랩 Task 목록

현재 리포의 완성된 코드(`d8b05fd feat: 주문-잔고 서비스 구현`)를 DAG 로 분해한 예시. Phase 2 에서 `scripts/replay-from-history.sh` 가 이 task 들을 이미 `done` 상태로 초기화한다. 신규 task 는 이 아래에 append.

> **Sentinel**: 이 yaml 블록의 첫 줄 `# harness:plan-tasks` 는 `plan-to-json.py` 가 **여러 yaml 펜스 중 실제 task 목록을 식별**하는 마커다. 지우지 말 것.

```yaml
# harness:plan-tasks
tasks:
  - id: SHARED-001
    title: "Shared domain — Money / AssetPair / DomainEvent / TxManager / EventProducer"
    deps: []
    files:
      creates:
        - internal/shared/domain/event.go
        - internal/shared/domain/money.go
        - internal/shared/domain/asset_pair.go
        - internal/shared/domain/tx.go
        - internal/shared/domain/producer.go
    exit_criteria:
      - hook: arch-check
        rules: [DDD-1, DDD-2, PERF-5]
      - hook: go-build
        packages: [./internal/shared/domain/...]

  - id: SHARED-002
    title: "Shared infra — SqlxTxManager + SaramaProducer facade"
    deps: [SHARED-001]
    files:
      creates:
        - internal/shared/infrastructure/tx.go
        - internal/shared/infrastructure/sarama_producer.go
    exit_criteria:
      - hook: arch-check
        rules: [FACADE-1]
      - hook: go-build
        packages: [./internal/shared/...]

  - id: BALANCE-001
    title: "Balance domain — aggregate + events + repo interface + errors"
    deps: [SHARED-001]
    files:
      creates:
        - internal/balance/domain/balance.go
        - internal/balance/domain/events.go
        - internal/balance/domain/repository.go
        - internal/balance/domain/errors.go
    exit_criteria:
      - hook: arch-check
        rules: [DDD-1, DDD-2, DDD-4, PERF-5]

  - id: BALANCE-002
    title: "Balance MySQL repo — SELECT FOR UPDATE + outbox drain in same Tx"
    deps: [BALANCE-001, SHARED-002]
    files:
      creates: [internal/balance/infrastructure/mysql_balance_repo.go]
    exit_criteria:
      - hook: arch-check
        rules: [EDA-3]

  - id: ORDER-001
    title: "Order domain — aggregate + state machine + events + repo interface"
    deps: [SHARED-001]
    files:
      creates:
        - internal/order/domain/order.go
        - internal/order/domain/events.go
        - internal/order/domain/repository.go
        - internal/order/domain/errors.go
    exit_criteria:
      - hook: arch-check
        rules: [DDD-1, DDD-2, DDD-4, MSA-1, PERF-5]

  - id: ORDER-002
    title: "Order MySQL repo — Save with outbox event drain"
    deps: [ORDER-001, SHARED-002]
    files:
      creates: [internal/order/infrastructure/mysql_order_repo.go]
    exit_criteria:
      - hook: arch-check
        rules: [EDA-3]

  - id: ORDER-003
    title: "PlaceOrderUseCase — pessimistic lock, idempotency, cross-BC orchestration"
    deps: [ORDER-002, BALANCE-002]
    files:
      creates:
        - internal/order/application/place_order.go
        - internal/order/application/cancel_order.go
    exit_criteria:
      - hook: arch-check
        rules: [MSA-1, EDA-3]
      - hook: go-test
        tests: [TestPlaceOrder_Unit]

  - id: OUTBOX-001
    title: "Outbox repository + MySQL impl (FOR UPDATE SKIP LOCKED)"
    deps: [SHARED-002]
    files:
      creates:
        - internal/outbox/repository.go
        - internal/outbox/mysql_outbox_repo.go
    exit_criteria:
      - hook: arch-check
        rules: [RELAY-1]

  - id: OUTBOX-002
    title: "Relay worker — exponential backoff + stuck detection"
    deps: [OUTBOX-001]
    files:
      creates: [internal/outbox/relay.go]
    exit_criteria:
      - hook: arch-check
        rules: [RELAY-1, RELAY-5, FACADE-1]

  - id: PRESENT-001
    title: "Order HTTP handler + Kafka consumer (DLQ + idempotent)"
    deps: [ORDER-003, OUTBOX-002]
    files:
      creates:
        - internal/order/presentation/handler.go
        - internal/order/presentation/dto.go
        - internal/order/presentation/kafka_consumer.go
    exit_criteria:
      - hook: arch-check
        rules: [MSA-3, EDA-1, EDA-2]

  - id: WIRE-001
    title: "cmd/server/main.go — DI wiring, graceful shutdown, relay goroutines"
    deps: [PRESENT-001]
    files:
      creates: [cmd/server/main.go, internal/config/config.go]
    exit_criteria:
      - hook: go-build
        packages: [./cmd/server/...]

  - id: TEST-001
    title: "Acceptance — happy path + insufficient balance + cancellation"
    deps: [WIRE-001]
    files:
      creates:
        - test/acceptance/support/suite_test.go
        - test/acceptance/support/acceptance_test.go
        - test/acceptance/features/order_placement.feature
        - test/acceptance/features/insufficient_balance.feature
        - test/acceptance/features/order_cancellation.feature
    exit_criteria:
      - hook: go-test
        tests: [TestOrderPlacement_HappyPath, TestInsufficientBalance, TestOrderCancellation]

  - id: TEST-002
    title: "Acceptance — concurrent orders (10-goroutine barrier) + outbox guarantee + idempotency"
    deps: [TEST-001]
    files:
      creates:
        - test/acceptance/support/concurrent.go
        - test/acceptance/support/wait.go
        - test/acceptance/features/concurrent_orders.feature
    exit_criteria:
      - hook: go-test
        tests: [TestConcurrentOrders, TestOutboxGuarantee, TestIdempotency]
```

---

## 7. 실행 아티팩트 매핑 (Phase 2 구현 대상)

이 문서의 스키마·규칙을 실제로 돌리는 코드/파일의 **확정 경로와 시그니처**. Phase 2 는 이 표를 전사(transcription)로 구현한다 (재설계 금지).

| 아티팩트 | 경로 | 입력 | 출력 / 부작용 | 담당 규칙 |
|---|---|---|---|---|
| `state.json` | `.claude/harness/state.json` | — | §2 스키마 준수 JSON. git 추적 | §2, §3 |
| `next-task.sh` | `.claude/harness/scripts/next-task.sh` | `state.json` + 이 문서 `tasks:` 블록 | stdout: `task_id=<id>\nfiles=<csv>\nexit_criteria=<json>\n` | §4 알고리즘 |
| `commit-and-advance.sh` | `.claude/harness/scripts/commit-and-advance.sh` | `state.json`, 현재 working tree | git commit (§5 형식), `state.json` `in_progress→done` | §3, §5 |
| `escalate.sh` | `.claude/harness/scripts/escalate.sh` | `$1=task_id $2=reason $3=attempts` | stdout: `HARNESS_ESCALATION task=… reason=… attempts=…`; state `in_progress→blocked` | §3 전이 |
| `validate-plan.sh` | `.claude/harness/scripts/validate-plan.sh` | 이 문서 | exit 0/1. 검증: id 유일, DAG acyclic, rules 존재, hooks 존재, files glob 유효 | §1 제약 |
| `replay-from-history.sh` | `.claude/harness/scripts/replay-from-history.sh` | `git log` trailer 들 | `state.json` 재구성 | §5 trailer contract |
| `reconcile-plan.sh` | `.claude/harness/scripts/reconcile-plan.sh` | plan 편집 후 | `plan_sha256` 갱신, 새 task → pending, 삭제된 task → 드롭 | §2 `plan_sha256` |
| `unblock.sh` | `.claude/harness/scripts/unblock.sh` | `$1=task_id` | `blocked→pending`, `failure_streak=null` | §3 전이 |

**Phase 2 검증:** `.claude/harness/scripts/validate-plan.sh` 가 CI 에서 PR 마다 돌아 이 문서와 state.json 의 불변식 검사.
