# 3단계: Hook (Hook Inventory + settings.json Contract)

> **역할**: 하네스의 "자동 검증 + 상태 전이"를 실제로 실행하는 **배선 계약**.
> - Claude Code 의 `settings.json` hooks 기능 위에서 동작.
> - 에이전트가 도구를 쓸 때마다 자동으로 불리고, 결과를 약속된 stdout 문법(§3)으로 내뱉는다.
> - 실패 출력은 `04_Fix.md` 의 recipe lookup 키로 직접 쓰인다.

---

## 1. Hook Inventory

| name | trigger event | matcher | script | concurrency | blocking |
|---|---|---|---|---|---|
| `inject-context` | `UserPromptSubmit` | — | `.claude/harness/scripts/next-task.sh` | single | non-blocking |
| `check-on-edit` | `PostToolUse` | `Write\|Edit\|MultiEdit` | `.claude/harness/scripts/check.sh $CLAUDE_TOOL_INPUT_file_path` | serial per-file | non-blocking (fail 은 Fix 루프로) |
| `commit-and-advance` | `Stop` | — | `.claude/harness/scripts/commit-and-advance.sh` | single | blocking |
| `validate-plan` | `PreToolUse` | `Write\|Edit` on `docs/01_Plan.md` | `.claude/harness/scripts/validate-plan.sh` | single | blocking (fail 이면 편집 차단) |
| `fix-dispatch` | `UserPromptSubmit` | — | `.claude/harness/scripts/last-failure-context.sh` | single | non-blocking |

**용어:**
- *trigger event* = Claude Code hook 종류.
- *matcher* = 해당 이벤트에서 어떤 tool/path 에만 발동할지.
- *blocking* = hook 실패가 에이전트 동작을 막는지 (PreToolUse 의 non-zero exit 은 tool 실행 차단).
- *concurrency* = 동시 여러 도구 호출이 있을 때 hook 의 실행 방식.

---

## 2. 각 Hook 동작

### 2.1 `inject-context` (UserPromptSubmit → next-task.sh)

**목적:** 매 turn 시작 시 `state.json` 의 현재 task + exit_criteria 를 에이전트 컨텍스트에 주입.

**stdout 계약** (에이전트가 시스템 컨텍스트로 받음):
```
<harness-context>
current_task: ORDER-003
title: PlaceOrderUseCase — pessimistic lock, idempotency, cross-BC orchestration
state: in_progress
attempts: 2
files:
  creates:
    - internal/order/application/place_order.go
    - internal/order/application/cancel_order.go
exit_criteria:
  - hook: arch-check
    rules: [MSA-1, EDA-3]
  - hook: go-test
    tests: [TestPlaceOrder_Unit]
last_failure:
  reason: ddd-1
  file: internal/order/application/place_order.go:12
  msg: "application imports infrastructure directly"
recipe_hint: see docs/04_Fix.md § recipe[ddd-1]
</harness-context>
```

**exit code:**
- 0: 출력된 context 를 주입
- 1: state.json 없음 → 초기화 필요 (에이전트에게 안내)
- 2: 플랜 손상 → HALT

### 2.2 `check-on-edit` (PostToolUse on Write|Edit|MultiEdit → check.sh)

**목적:** 편집 직후 해당 파일 범위의 패턴만 빠르게 검사. 전체 재검사(`check.sh --all`)는 Stop hook 에서.

**입력:** `$CLAUDE_TOOL_INPUT_file_path` — 방금 편집된 파일 (Claude Code hook 규약).

**stdout 문법** (한 줄당 한 결과):
```
HARNESS_HOOK_PASS name=<rule-id>
HARNESS_HOOK_FAIL name=<rule-id> reason=<reason-key> file=<path>[:<line>] [msg="<short>"]
```

여러 규칙 동시 검사 시 여러 줄 출력. 파서(Fix.md 와 escalate.sh)는 **라인 단위**로 파싱.

**예시 (성공):**
```
HARNESS_HOOK_PASS name=DDD-1
HARNESS_HOOK_PASS name=DDD-2
HARNESS_HOOK_PASS name=EDA-3
```

**예시 (실패):**
```
HARNESS_HOOK_PASS name=DDD-2
HARNESS_HOOK_FAIL name=DDD-1 reason=ddd-1 file=internal/order/domain/order.go:5 msg="domain imports infrastructure"
HARNESS_HOOK_FAIL name=PERF-5 reason=perf-5 file=internal/order/domain/order.go:42 msg="float64 in financial field"
```

**부작용:** 첫 FAIL 이 있으면 `.claude/harness/last-failure.json` 을 해당 실패로 덮어쓴다 (§4).

**exit code:**
- 0: 모든 검사 PASS
- 1: 1개 이상 FAIL
- 2: infra error (grep 없음, 쉘 크래시 등) — task 실패로 카운트하지 않음

### 2.3 `commit-and-advance` (Stop → commit-and-advance.sh)

**목적:** turn 종료 시 전체 검증 + 커밋 + 상태 전이.

**절차:**
1. 현재 task 가 없으면 skip (no-op).
2. `check.sh --all` 실행 → 현재 task 의 `exit_criteria` 범위 전체 검증.
3. 모두 PASS 시:
   - working tree 에 변경 있으면 `git add` → §5 형식으로 `git commit`.
   - `state.json` 의 current task `in_progress → done`, `commit_sha` 기록, `current_task ← null`.
   - stdout: `HARNESS_STATE_TRANSITION task=<id> from=in_progress to=done`.
4. 일부 FAIL 시:
   - **commit 하지 않음** (WIP 으로 남김).
   - `failure_streak` 갱신 (§4).
   - `count >= 3` 이면 `escalate.sh` 호출 (§2.4).
   - stdout: 각 실패의 `HARNESS_HOOK_FAIL` 줄 + `HARNESS_TURN_INCOMPLETE task=<id>`.
5. 다음 eligible task 가 있으면 로그에 힌트만 남김. 자동 선택은 다음 turn 의 `inject-context` 가 함.

**exit code:**
- 0: 커밋됨 또는 합리적 no-op
- 1: FAIL 존재 (다음 turn 에 Fix 루프)
- 2: git 에러 (커밋 실패) → blocking, 사람 확인 필요

### 2.4 `validate-plan` (PreToolUse on docs/01_Plan.md)

**목적:** Plan.md 의 `tasks:` 블록이 편집될 때 DAG 불변식을 즉시 검증. 깨진 Plan 으로 Write 되는 걸 막음.

**검증 항목** (01_Plan.md §1 + §3 불변식):
- id 유일성
- deps 참조가 전부 존재
- DAG acyclic (DFS)
- `rules` 가 02_Code.md patterns.tsv 에 존재
- `hook` 이름이 이 문서 §1 인벤토리에 존재
- `files.creates` glob 이 실제 파일 시스템 경로로 표현 가능

**exit code:**
- 0: 통과 → Write 진행
- 1: FAIL → PreToolUse 가 non-zero 로 edit 차단. stdout 에 `HARNESS_PLAN_INVALID reason=<detail>` 출력.

### 2.5 `fix-dispatch` (UserPromptSubmit → last-failure-context.sh)

**목적:** 직전 turn 에서 실패가 있었으면, 해당 `reason_key` 의 레시피 요약을 컨텍스트에 추가로 inject.

**stdout:** `last-failure.json` 이 있으면 아래 블록:
```
<fix-context>
last_failure: { reason: "ddd-1", file: "...", attempts: 2 }
recipe:
  detect: grep '".*infrastructure' internal/*/domain/
  steps:
    1. Move the imported type's interface to domain/, keep impl in infrastructure/.
    2. Update imports to reference the interface only.
  verify: .claude/harness/scripts/check.sh --only ddd-1 <file>
escalation_in: 1  (attempts=2, threshold=3)
</fix-context>
```

`inject-context` 와 독립적으로 실행. 둘 다 non-blocking UserPromptSubmit → Claude Code 는 두 출력을 모두 컨텍스트에 주입.

---

## 3. 출력 문법 (HARNESS_ grammar)

모든 hook 은 stdout 에 아래 문법만 써야 한다. 다른 출력은 stderr 로 보낸다 (로그). 파서는 라인 단위로 regex 매칭.

```
# PASS (optional — 로깅용)
HARNESS_HOOK_PASS name=<rule-id>

# FAIL
HARNESS_HOOK_FAIL name=<rule-id> reason=<reason-key> [file=<path>[:<line>]] [msg="<short>"]

# 상태 전이 (commit-and-advance.sh 만 emit)
HARNESS_STATE_TRANSITION task=<task-id> from=<state> to=<state> [commit=<sha>]

# 에스컬레이션 (escalate.sh 만 emit)
HARNESS_ESCALATION task=<task-id> reason=<reason-key> attempts=<n>

# Plan 편집 거부 (validate-plan.sh 만 emit)
HARNESS_PLAN_INVALID reason=<detail>

# turn 미완료 (commit 없이 종료)
HARNESS_TURN_INCOMPLETE task=<task-id>

# scope 경고 (non-blocking — commit 은 scope 안 파일만 포함)
HARNESS_SCOPE_WARNING reason=<key> [count=<n>] [files=<csv>]
```

**파싱 regex** (Phase 2 파서가 기대하는 것):
```
^HARNESS_(HOOK_(PASS|FAIL)|STATE_TRANSITION|ESCALATION|PLAN_INVALID|TURN_INCOMPLETE)\s+(.+)$
```

key=value 파싱 규칙:
- 값에 공백 있으면 `"..."` 로 감쌈.
- unquoted 값은 `[^\s]+`.
- 모든 key 는 snake_case ASCII.
- 키 순서는 무관 (파서는 map 으로 처리).

---

## 4. `last-failure.json` 스키마

경로: `.claude/harness/last-failure.json` (한 개 파일, 가장 최근 FAIL 로 덮어쓰기).

```json
{
  "timestamp": "2026-04-16T10:25:00Z",
  "task_id": "ORDER-003",
  "rule_id": "DDD-1",
  "reason": "ddd-1",
  "file": "internal/order/application/place_order.go",
  "line": 12,
  "msg": "application imports infrastructure directly",
  "tool": "Edit",
  "attempts_before": 1
}
```

**부작용 규약:**
- `check-on-edit` 가 FAIL 을 emit 하면 이 파일을 overwrite.
- `commit-and-advance` 가 성공하면 이 파일을 삭제.
- `fix-dispatch` 는 이 파일을 읽기만 함.

---

## 5. `.claude/settings.json` 배선 (copy-paste 가능한 정답)

Phase 2 는 이 JSON 을 그대로 repo 의 `.claude/settings.json` 에 넣고 필요한 스크립트만 채운다.

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          { "type": "command", "command": ".claude/harness/scripts/next-task.sh" },
          { "type": "command", "command": ".claude/harness/scripts/last-failure-context.sh" }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          { "type": "command", "command": ".claude/harness/scripts/validate-plan.sh \"$CLAUDE_TOOL_INPUT_file_path\"" }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          { "type": "command", "command": ".claude/harness/scripts/check.sh \"$CLAUDE_TOOL_INPUT_file_path\"" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": ".claude/harness/scripts/commit-and-advance.sh" }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(.claude/harness/scripts/*.sh:*)"
    ]
  }
}
```

**변수 규약 (Claude Code 제공):**
- `$CLAUDE_TOOL_INPUT_file_path` — Write/Edit 의 target path.
- `$CLAUDE_CWD` — 프로젝트 루트.

**주의:** `validate-plan.sh` 는 편집 대상이 `docs/01_Plan.md` 일 때만 실제 검증을 돌리고, 그 외엔 exit 0. matcher 에 path 매처를 넣을 수 없으므로 스크립트 내부에서 분기.

---

## 6. Hook 실행 순서

같은 이벤트에 여러 hook 이 매치될 때:

1. **UserPromptSubmit**: 배열 순서대로 `next-task` → `last-failure-context`. 둘 다 stdout 에 context 블록을 쓰고 에이전트 컨텍스트에 병합.
2. **PreToolUse**: `validate-plan` 하나. non-zero exit → tool 차단.
3. **PostToolUse**: `check-on-edit` 하나. 비동기로 실행되어도 다음 Stop 전까지 완료 보장.
4. **Stop**: `commit-and-advance` 하나. 가장 무거운 작업 (전체 arch-check + test + commit).

**동시성:** PostToolUse 가 같은 파일에 여러 번 매치될 경우 (MultiEdit), check.sh 는 마지막 호출만 유효. 내부적으로 flock 으로 직렬화.

---

## 7. 실행 아티팩트 매핑 (Phase 2 구현 대상)

| 아티팩트 | 경로 | 역할 | 참조 계약 |
|---|---|---|---|
| Hook 배선 | `.claude/settings.json` | §5 JSON 을 그대로 materialize | Claude Code hook spec |
| `next-task.sh` | `.claude/harness/scripts/next-task.sh` | §2.1 | 01_Plan.md §4 알고리즘 |
| `last-failure-context.sh` | `.claude/harness/scripts/last-failure-context.sh` | §2.5 | §4 schema + 04_Fix.md recipe 테이블 |
| `validate-plan.sh` | `.claude/harness/scripts/validate-plan.sh` | §2.4 | 01_Plan.md §1 제약 |
| `check.sh` | `.claude/harness/scripts/check.sh` | §2.2 + §3 출력 문법 | 02_Code.md patterns.tsv |
| `commit-and-advance.sh` | `.claude/harness/scripts/commit-and-advance.sh` | §2.3 | 01_Plan.md §3 + §5 |
| `escalate.sh` | `.claude/harness/scripts/escalate.sh` | §3 `HARNESS_ESCALATION` | 01_Plan.md §3, 04_Fix.md §3 |
| `last-failure.json` | `.claude/harness/last-failure.json` | §4 schema | — |
| 로그 | `.claude/harness/logs/hook-{date}.log` | stderr 수집 (append-only) | — |

**Phase 2 검증:**
- §5 JSON 이 Claude Code 의 schema 와 충돌 없음 (실제 `/settings/hooks` 로 load 되는지 smoke test).
- §3 grammar 에 대한 파서 테스트 (허용/거부 예시 각 5개씩).
- 각 스크립트가 exit code 규약 (0/1/2) 을 정확히 따르는지 shunit2 등으로 테스트.
