# 4단계: Fix (reason → Recipe Lookup + Escalation)

> **역할**: `03_Hook.md § 2.2` 의 check-on-edit 이 `HARNESS_HOOK_FAIL reason=<key>` 를 뱉을 때마다, 에이전트가 **같은 `<key>`** 로 이 문서의 §2 테이블을 lookup → recipe 의 steps 를 그대로 실행 → verify cmd 로 확인.
>
> 즉 Fix 는 **판단이 아니라 디스패치**다. 에이전트가 "아 이걸 어떻게 고치지" 를 사고하는 게 아니라, 문서가 "reason=X → 이 단계 실행" 을 테이블로 제공.
>
> 설계 배경(규칙의 이유)은 `00_Design.md §5`.

---

## 1. 디스패치 루프

```
PostToolUse hook → check.sh → HARNESS_HOOK_FAIL reason=<key> file=<path>:<line>
  ↓
last-failure.json 갱신 (Hook.md §4)
  ↓
[다음 turn] UserPromptSubmit → last-failure-context.sh → 에이전트 컨텍스트에
  recipe[<key>] 요약을 inject
  ↓
에이전트: recipe.steps 실행 → recipe.verify 실행
  ↓
verify PASS → 다음 편집 계속. Stop hook 에서 commit-and-advance 가 나머지 처리.
verify FAIL → attempts++. 동일 reason 3회 도달 시 escalate.sh → blocked.
```

---

## 2. Recipe 테이블

각 행은 `.claude/harness/recipes/<reason>.md` 로 materialize (Phase 2). 여기 테이블은 **요약 view** — 풀 버전은 개별 recipe 파일.

| reason_key | detect cmd | fix steps (순서대로) | verify cmd | max retries |
|---|---|---|---|---|
| `ddd-1` | `grep -rn '".*infrastructure' internal/*/domain/` | 1. 매치된 파일에서 `infrastructure` import 제거. 2. 해당 타입이 정말 도메인에서 필요하면, interface 를 `domain/` 에 정의하고 impl 은 `infrastructure/` 에 둠 (DDD-4). 3. 기존 구체 타입 참조를 interface 로 교체. | `.claude/harness/scripts/check.sh --only DDD-1 <file>` | 3 |
| `ddd-2` | `grep -rnE '"(database/sql\|github.com/jmoiron/sqlx\|github.com/go-sql-driver)"' internal/*/domain/` | 1. domain/ 파일에서 DB driver import 제거. 2. 해당 타입이 필요한 연산을 repository interface 의 메서드로 추출 (`domain/repository.go`). 3. 구현을 `infrastructure/mysql_*_repo.go` 로 이동. | 위 동일 (`--only DDD-2`) | 3 |
| `ddd-4` | `ls internal/*/domain/repository.go && ls internal/*/infrastructure/mysql_*_repo.go` | 1. repository interface 가 `domain/` 에 없으면 추가. 2. `Mysql*Repository` struct 가 `infrastructure/` 에 없으면 추가 (Code.md §3.2, §3.3 템플릿). 3. `main.go` DI 에서 interface 주입 확인. | 위 동일 (`--only DDD-4`) | 2 |
| `msa-1` | `grep -rn '"github.com/HongJungWan/harness-engineering/internal/balance' internal/order/` | 1. order/ 에서 balance 직접 import 제거. 2. 필요한 호출을 `order/application/` usecase 로 끌어와 생성자 주입 (constructor injection) 으로 교체. 3. interface 는 order/application/ 에 두고, balance 측 구현체가 이를 만족. | 위 동일 (`--only MSA-1`) | 3 |
| `msa-3` | `grep -rnE 'json\.Marshal\([^)]*domain\.' internal/*/presentation/handler.go` | 1. domain entity 를 그대로 marshal 하는 곳 특정. 2. `presentation/dto.go` 에 Response DTO 추가. 3. handler 에서 `DTO{...}` 로 mapping 후 marshal. | 위 동일 (`--only MSA-3`) | 2 |
| `eda-1` | `grep -cE '(DLQTopic\|DLQProducer\|publishToDLQ)' internal/order/presentation/kafka_consumer.go` | 1. `DLQProducer` 의존성을 consumer struct 에 추가. 2. message handler error 시 `publishToDLQ(ctx, msg, err)` 호출 후 `return nil` (offset commit). 3. config 에 `KAFKA_DLQ_TOPIC` 연결 확인. | 위 동일 (`--only EDA-1`) | 2 |
| `eda-2` | `grep -c 'processed_events' internal/order/presentation/kafka_consumer.go` | 1. `ProcessedEventsRepository` 의존성 추가. 2. ConsumeClaim 에서 `TxManager.RunInTx` 로 감싸고 그 안에서 (a) `Exists(tx, event_id, group)` 체크 → true 면 dedup, (b) handler 실행, (c) `Insert(tx, event_id, group)`. | 위 동일 (`--only EDA-2`) | 2 |
| `eda-3` | `grep -B2 -A2 'INSERT INTO outbox_events' internal/*/infrastructure/` — `tx.ExecContext` 가 같은 블록에 없으면 FAIL | 1. `Save(ctx, entity)` 안에서 entity.PullEvents() 루프로 outbox insert 수행. 2. 모든 INSERT 를 `ctx` 에 박힌 tx 로 실행 (`sharedinfra.TxFromContext(ctx)` 또는 동일 repo 의 `*sqlx.Tx` 사용). 3. entity UPSERT 와 동일한 Tx 인지 재확인. | 위 동일 (`--only EDA-3`) | 3 |
| `relay-1` | `grep -rn 'SKIP LOCKED' internal/outbox/` | 1. relay 의 SELECT 쿼리에 `FOR UPDATE SKIP LOCKED` 추가. 2. 동시에 `WHERE status='PENDING' ORDER BY id ASC LIMIT :n` 유지 확인. 3. `idx_status_id` 인덱스 존재 (scripts/init.sql) 확인. | 위 동일 (`--only RELAY-1`) | 2 |
| `relay-5` | `grep -nE '(CountStuckEvents\|detectStuckEvents\|stuck)' internal/outbox/relay.go` | 1. relay.go 에 `detectStuckEvents()` 메서드 추가 (`WHERE status='PENDING' AND created_at < NOW(6) - INTERVAL 5 MINUTE`). 2. 60초 주기 ticker 로 실행. 3. stuck 발견 시 경고 로그 + 메트릭 increment. | 위 동일 (`--only RELAY-5`) | 2 |
| `facade-1` | `grep -n '"github.com/IBM/sarama"' internal/outbox/relay.go` | 1. relay.go 에서 sarama import 제거. 2. `shareddomain.EventProducer` interface 만 사용. 3. 구체 Sarama 구현은 `shared/infrastructure/sarama_producer.go` 에 있고 cmd/server/main.go 에서 주입됨을 확인. | 위 동일 (`--only FACADE-1`) | 2 |
| `perf-5` | `grep -rnE '\bfloat(32\|64)\b' internal/*/domain/ \| grep -v _test.go` | 1. 모든 float 필드/변수를 `decimal.Decimal` 로 교체. 2. 리터럴은 `decimal.NewFromString("0.1")` 또는 `decimal.NewFromInt(n)` 사용. 3. 연산은 `.Add` / `.Sub` / `.Mul` / `.Div` / `.Cmp`. | 위 동일 (`--only PERF-5`) | 3 |
| `go-build` | `go build ./... 2>&1` | 1. compiler 에러 메시지의 파일:줄 로 직행. 2. 가장 최근 편집 파일의 signature/import 먼저 재확인. 3. `go mod tidy` 로 모듈 정합성 확인. | `go build ./...` (exit 0) | 3 |
| `go-test` | `go test ./... -run <name>` 의 실패 출력 | 1. 실패한 테스트의 `--- FAIL:` 덤프 확인. 2. 단일 테스트 격리: `go test -run <name> -v -count=1`. 3. assertion 기대값 vs 실제값 분석. 4. 도메인 침범/경계 문제면 recipe 의 역계산: 패턴 FAIL 이 숨어 있지 않은지 check.sh 전체 재실행. | 동일 (exit 0) | 3 |
| `go-vet` | `go vet ./...` | 1. vet 경고 메시지 확인. 2. 전형적: shadowed variable, unused result, composite literal. 3. 직진 수정. | 동일 (exit 0) | 2 |
| `go-lint` | `golangci-lint run ./...` | 1. linter 카테고리별로 처리 (errcheck, staticcheck, ineffassign 등). 2. auto-fix 가능한 건 `golangci-lint run --fix`. | 동일 (exit 0) | 2 |

**테이블 부재 reason (catch-all):** 위 표에 없는 `reason` 을 받으면:
- 에이전트는 `HARNESS_ESCALATION task=<current> reason=<unknown> attempts=1` 을 stdout 에 내고, 같은 reason 에 대한 recipe 를 Fix.md 에 **추가 제안** 하는 PR 을 준비한다 (task 는 blocked 로 마킹).

---

## 3. Escalation 규칙

### 3.1 Failure streak 카운팅

`state.json.tasks[<id>].failure_streak` 가 단일 reason 에 대해서만 누적된다. **다른 reason 으로 실패하면 카운터가 리셋**.

```python
def update_streak(state, task_id, new_reason):
    streak = state.tasks[task_id].get("failure_streak", None)
    if streak is None or streak["reason"] != new_reason:
        state.tasks[task_id]["failure_streak"] = {"reason": new_reason, "count": 1}
    else:
        state.tasks[task_id]["failure_streak"]["count"] += 1
    return state.tasks[task_id]["failure_streak"]["count"]
```

**이유:** 다른 reason 으로 실패한다면 Fix 루프가 **진전**하고 있다는 증거 (이전 reason 은 해결되었고 새 reason 이 드러남). 동일 reason 반복만이 "이 recipe 로 못 고치고 있다" 신호.

### 3.2 임계치 & 전이

| 임계치 | 액션 |
|---|---|
| `count == 3` (동일 reason) | `escalate.sh` 호출. `in_progress → blocked`. `current_task ← null`. |
| 전체 blocked 비율 >= 50% | 전역 HALT. 사람 개입 필요. |
| 24h 이상 동일 task 가 `blocked` | (옵션) 자동 `pending` 으로 되돌려 재시도 허용 — Phase 2 에서 결정 |

### 3.3 `escalate.sh` 출력

```
HARNESS_ESCALATION task=ORDER-003 reason=ddd-1 attempts=3
```

부작용:
- `state.json` 의 `current_task` ← null
- 해당 task `state` ← `blocked`
- `.claude/harness/blocked/<task_id>.md` 에 실패 로그 덤프 (last-failure.json 아카이브 + `git diff` + 마지막 3 턴 요약)

### 3.4 사람의 unblock 프로토콜

사람이 blocked 를 풀 때:
1. `.claude/harness/blocked/<task_id>.md` 를 읽어 상황 파악.
2. 문제가 recipe 의 결함이라면 Fix.md §2 의 해당 recipe 를 고치고 커밋.
3. `.claude/harness/scripts/unblock.sh <task_id>` 실행 → `blocked → pending`, `failure_streak = null`.
4. 다음 turn 에 에이전트가 pick-up.

---

## 4. Recipe 품질 불변식

새 recipe 를 추가할 때 다음을 만족해야 한다 (`.claude/harness/scripts/validate-recipes.sh` 가 검증):

1. **Detect cmd 가 있다**: 그 reason 이 현재 발생 중인지 단일 명령으로 재현 가능.
2. **Fix steps 가 결정론적이다**: 각 단계가 "X 를 하라" 수준으로 구체. "적절히 판단" 금지.
3. **Verify cmd 는 detect 의 inverse**: verify 가 PASS 여야 그 reason 이 해결된 것.
4. **Max retries 가 정의**: 보통 2~3. recipe 가 부실할수록 숫자를 낮게.
5. **00_Design.md 근거 링크**: 왜 이 규칙이 있는지 "§5.x" 형태로 참조.

---

## 5. 리뷰 단계와의 관계

과거 04_Fix.md 에 있던 "아키텍처 리뷰 추가 점검 항목" (최종 일관성, 보상 트랜잭션, MSA 로드맵 준수) 은 **Fix 루프의 대상이 아니다** — 이건 사람 리뷰어의 책임. → `05_Review.md` 로 이전 (Phase 2 에서 개정).

Fix.md 는 "실패가 있고 그걸 코드 수정으로 자동 해결" 만 담당.

---

## 6. 실행 아티팩트 매핑 (Phase 2 구현 대상)

| 아티팩트 | 경로 | 입력 | 출력 / 부작용 |
|---|---|---|---|
| Recipe 테이블 (요약) | 이 파일 §2 | — | `validate-recipes.sh` 가 파싱해 `.claude/harness/recipes.tsv` 생성 |
| 개별 recipe | `.claude/harness/recipes/<reason>.md` | reason_key | recipe 의 full narrative (검색 · 수정 · 검증 스니펫, 근거) |
| `apply-fix.sh` | `.claude/harness/scripts/apply-fix.sh <reason>` | reason_key | recipes/<reason>.md 를 에이전트 컨텍스트에 inject. 자동 실행은 하지 않음 (에이전트가 읽고 수행) |
| `last-failure-context.sh` | `.claude/harness/scripts/last-failure-context.sh` | last-failure.json | §1 의 `<fix-context>` 블록 stdout (Hook.md §2.5 와 동일 경로) |
| `escalate.sh` | `.claude/harness/scripts/escalate.sh` | `$1=task $2=reason $3=attempts` | §3.3 stdout + state 전이 + blocked 로그 덤프 |
| `unblock.sh` | `.claude/harness/scripts/unblock.sh <task_id>` | task_id | §3.4 프로토콜 적용 |
| `validate-recipes.sh` | `.claude/harness/scripts/validate-recipes.sh` | 이 파일 | 모든 recipe 가 §4 불변식 만족하는지 검증. CI 에서 실행 |
| Blocked 덤프 | `.claude/harness/blocked/<task_id>.md` | escalate.sh | 사람 개입용 자료 |

**Phase 2 검증:**
- `validate-recipes.sh` 가 §2 의 모든 reason_key 가 `02_Code.md § patterns.tsv` 에도 존재함을 교차 검증 (양방향 완전 매칭).
- 각 recipe 에 detect/fix/verify 세 요소가 모두 있는지 YAML front-matter 로 구조화 검증 가능하게 할 것 (Phase 2 의 recipe 파일 스키마).
