# 하네스 엔지니어링 적용 가이드

> **40분 AI 코딩테스트**에서 하네스 엔지니어링을 적용하기 위한 실전 가이드.
> 사전에 3개 템플릿을 준비하고, 테스트 시작과 동시에 **8분 안에 자동 검증 루프를 세팅**한 뒤, 나머지 32분을 루프 안에서 코딩한다.

---

## 0. 치트시트 (테스트 당일 이것만 보고 따라하기)

### 세팅 (1분)

```bash
mkdir -p .claude/scripts && cp ~/harness-templates/CLAUDE.md . && cp ~/harness-templates/.claude/scripts/check.sh .claude/scripts/ && cp ~/harness-templates/.claude/settings.json .claude/ && chmod +x .claude/scripts/check.sh
```

### Phase 1 — 제약 정의 (0-5분)

```
이 문제를 분석해서 CLAUDE.md 의 [문제 개요], [디렉토리 구조], [금지 패턴],
[필수 패턴], [수정 레시피] placeholder 를 채워. grep 으로 검증 가능한 규칙만.
```

### Phase 2 — 규칙 구현 (5-8분)

```
CLAUDE.md 의 금지 패턴과 필수 패턴을 .claude/scripts/check.sh 에 bash 함수로 구현해.
기존 예시 함수(check_F1, check_F2, check_R1)는 삭제하고 새로 작성해.
각 함수는 위반 시 "FAIL [ID]: <설명>" 출력 + FAIL=1 설정.
```

### Phase 3 — 구현 (8-35분)

```
CLAUDE.md 의 제약을 전부 지키면서 <문제 요구사항> 을 구현해.
```

→ 이후 **자동**: 편집 → check.sh → 위반 시 Claude 가 즉시 수정 → 반복

### Phase 4 — 최종 검증 (35-40분)

```
.claude/scripts/check.sh --full 돌려서 전체 규칙 + 빌드 + 테스트 PASS 확인해. 실패하면 고쳐.
```

### 비상: check.sh 가 깨졌을 때

```bash
bash -n .claude/scripts/check.sh   # 문법 오류 확인
```

---

> 아래는 **사전 학습용 상세 설명**. 시험 당일에는 위 치트시트만 따라가면 됨.

---

## 1. 핵심 개념 (30초)

**일반 AI 코딩**: 프롬프트 → 코드 생성 → 사람이 리뷰

**하네스 엔지니어링**: 제약 선행 정의 → AI 가 제약 안에서 구현 → **매 편집마다 자동 검증** → 위반 시 자동 피드백 → 수정

차이는 하나: **자동 검증 루프가 있느냐 없느냐**. 프롬프트에 "DDD 를 지켜라" 라고 쓰면 희망사항이고, `check.sh` 가 매 편집마다 `grep` 으로 검사하면 강제.

---

## 2. 최소 실행 가능 하네스 (MVH)

40분 안에 세팅 가능한 **최소 3파일**:

| 파일 | 역할 | 세팅 시간 |
|---|---|---|
| `CLAUDE.md` | 제약 정의 + 수정 레시피 (Plan + Fix) | 5분 |
| `.claude/scripts/check.sh` | 자동 검증 규칙 (Hook) | 3분 |
| `.claude/settings.json` | Claude Code hook 배선 | 복사만 (10초) |

이 3개가 있으면 **모든 Write/Edit 에 `check.sh` 가 자동 실행**됨. 나머지 (state.json, task DAG, escalation 등) 는 40분 테스트에선 불필요.

---

## 3. 사전 준비물 (테스트 전)

아래 3개 템플릿(§5, §6, §7)을 **미리 로컬에 저장**해둔다. 테스트 시작 시 프로젝트 디렉토리에 복사하고 placeholder 만 채우면 끝.

```bash
# 사전 준비: 템플릿 보관 디렉토리
mkdir -p ~/harness-templates/.claude/scripts
# §5 의 CLAUDE.md  → ~/harness-templates/CLAUDE.md
# §6 의 check.sh   → ~/harness-templates/.claude/scripts/check.sh
# §7 의 settings.json → ~/harness-templates/.claude/settings.json
```

---

## 4. 40분 타임라인

### Phase 1 (0-5분): Plan — 문제 분석 + 제약 정의

문제를 읽고 `CLAUDE.md` 의 placeholder 를 채운다.

**채울 항목:**
1. `[문제 개요]` — 한 줄 요약
2. `[디렉토리 구조]` — 계층 분리 규칙
3. `[금지 패턴]` — grep 으로 검증 가능한 것 3-5개
4. `[필수 패턴]` — 있어야 하는 것 2-3개
5. `[수정 레시피]` — 위반 시 어떻게 고칠지 한 줄씩

**Claude Code 에게 시키는 프롬프트:**

```
이 문제를 분석해서 CLAUDE.md 의 [문제 개요], [디렉토리 구조], [금지 패턴],
[필수 패턴], [수정 레시피] placeholder 를 채워. grep 으로 검증 가능한 규칙만.
```

### Phase 2 (5-8분): Hook — 자동 검증 배선

CLAUDE.md 에 정의한 금지/필수 패턴을 `check.sh` 에 옮긴다.

**Claude Code 에게 시키는 프롬프트:**

```
CLAUDE.md 의 금지 패턴과 필수 패턴을 .claude/scripts/check.sh 에 bash 함수로 구현해.
기존 예시 함수(check_F1, check_F2, check_R1)는 삭제하고 새로 작성해.
각 함수는 위반 시 "FAIL [ID]: <설명>" 을 출력하고 FAIL 변수를 1로 설정해.
```

이 시점부터 `settings.json` 이 활성화되어 있으므로, 이후 모든 Write/Edit 에서 `check.sh` 가 **자동으로 실행**된다.

### Phase 3 (8-35분): Code + Fix 루프 (자동)

**Claude Code 에게 시키는 프롬프트:**

```
CLAUDE.md 의 제약을 전부 지키면서 <문제 요구사항> 을 구현해.
```

이 구간에서 일어나는 일:
1. Claude 가 파일을 편집
2. PostToolUse hook 이 `check.sh` 자동 실행
3. FAIL 이 나오면 Claude 가 출력을 보고 **즉시 수정** (Fix)
4. PASS 가 나올 때까지 반복

**사용자는 지켜보기만 하면 됨.** 규칙 위반이 발생하면 자동으로 피드백되어 수정됨.

### Phase 4 (35-40분): 최종 검증 + 정리

**Claude Code 에게 시키는 프롬프트:**

```
.claude/scripts/check.sh --full 돌려서 전체 규칙 + 빌드 + 테스트 PASS 확인해.
실패하면 고쳐.
```

커밋하고 제출.

---

## 5. 템플릿 1: CLAUDE.md

아래를 프로젝트 루트에 복사하고 `[...]` placeholder 를 채운다.

```markdown
# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 문제 개요

[문제 설명 1-2줄]

## 빌드 / 테스트

[언어에 맞게 수정 — 예: `go build ./... && go test ./...` / `npm run build && npm test` / `pytest`]

## 디렉토리 구조

[프로젝트 계층 — 아래는 DDD 예시]

## 아키텍처 제약 (편집할 때마다 .claude/scripts/check.sh 가 자동 실행됨)

### 금지 패턴

| ID | 패턴 | 적용 범위 | 이유 |
|---|---|---|---|
| F-1 | [금지할 import/패턴] | [어떤 디렉토리] | [왜 금지] |
| F-2 | ... | ... | ... |
| F-3 | ... | ... | ... |

### 필수 패턴

| ID | 패턴 | 적용 범위 | 이유 |
|---|---|---|---|
| R-1 | [있어야 할 패턴] | [어떤 파일] | [왜 필수] |
| R-2 | ... | ... | ... |

## 위반 시 수정 레시피

| 실패 ID | 수정 방법 |
|---|---|
| F-1 | [구체적 수정 절차] |
| F-2 | ... |
| R-1 | ... |
```

---

## 6. 템플릿 2: `.claude/scripts/check.sh`

아래를 `.claude/scripts/check.sh` 로 복사하고 `chmod +x`. 규칙 함수를 문제에 맞게 추가/수정.

```bash
#!/usr/bin/env bash
# 하네스 자동 검증 스크립트.
# PostToolUse hook 에서 매 편집마다 자동 실행.
# 규칙을 추가하려면 check_XXX() 함수를 작성하고 아래 RULES 배열에 등록.
set -uo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

FAIL=0

# ╔════════════════════════════════════════════╗
# ║  여기에 프로젝트별 규칙 함수를 추가        ║
# ╚════════════════════════════════════════════╝

# ── 금지 패턴 예시 ──────────────────────────

check_F1() {
    # 예: domain 에서 infrastructure import 금지
    local hits
    hits=$(grep -rn '".*infrastructure' src/domain/ 2>/dev/null || true)
    if [[ -n "$hits" ]]; then
        echo "FAIL [F-1]: domain imports infrastructure"
        echo "$hits" | head -3
        FAIL=1
    else
        echo "PASS [F-1]"
    fi
}

check_F2() {
    # 예: 금융 연산에 float 사용 금지
    local hits
    hits=$(grep -rnE '\bfloat(32|64|)\b' src/domain/ 2>/dev/null | grep -v _test || true)
    if [[ -n "$hits" ]]; then
        echo "FAIL [F-2]: float in domain (use Decimal)"
        echo "$hits" | head -3
        FAIL=1
    else
        echo "PASS [F-2]"
    fi
}

# ── 필수 패턴 예시 ──────────────────────────

check_R1() {
    # 예: 모든 public handler 가 에러 핸들링을 포함
    if ! grep -rq 'error' src/presentation/ 2>/dev/null; then
        echo "FAIL [R-1]: presentation layer missing error handling"
        FAIL=1
    else
        echo "PASS [R-1]"
    fi
}

# ── 빌드/테스트 (느리므로 --full 플래그일 때만 실행) ──

check_BUILD() {
    if ! go build ./... 2>&1; then
        echo "FAIL [BUILD]"
        FAIL=1
    else
        echo "PASS [BUILD]"
    fi
}

# ╔════════════════════════════════════════════╗
# ║  규칙 실행                                 ║
# ║  FAST: 매 편집마다 (grep, 밀리초)          ║
# ║  FULL: --full 플래그일 때만 (빌드/테스트)  ║
# ╚════════════════════════════════════════════╝

FAST_RULES=(check_F1 check_F2 check_R1)
FULL_RULES=(check_BUILD)

for rule in "${FAST_RULES[@]}"; do "$rule"; done

if [[ "${1:-}" == "--full" ]]; then
    for rule in "${FULL_RULES[@]}"; do "$rule"; done
fi

if (( FAIL )); then
    echo ""
    echo "=== 규칙 위반 발견. CLAUDE.md 의 수정 레시피 참조 ==="
    exit 1
fi
echo ""
echo "=== 모든 규칙 통과 ==="
exit 0
```

### 언어별 빌드/테스트 함수 교체 예시

```bash
# Go
check_BUILD() { go build ./... 2>&1 || { echo "FAIL [BUILD]"; FAIL=1; }; }
check_TEST()  { go test -short ./... 2>&1 || { echo "FAIL [TEST]"; FAIL=1; }; }

# Python
check_BUILD() { python -m py_compile main.py 2>&1 || { echo "FAIL [BUILD]"; FAIL=1; }; }
check_TEST()  { pytest --tb=short 2>&1 || { echo "FAIL [TEST]"; FAIL=1; }; }

# TypeScript
check_BUILD() { npx tsc --noEmit 2>&1 || { echo "FAIL [BUILD]"; FAIL=1; }; }
check_TEST()  { npm test 2>&1 || { echo "FAIL [TEST]"; FAIL=1; }; }

# Java (Gradle)
check_BUILD() { ./gradlew build -x test 2>&1 || { echo "FAIL [BUILD]"; FAIL=1; }; }
check_TEST()  { ./gradlew test 2>&1 || { echo "FAIL [TEST]"; FAIL=1; }; }
```

---

## 7. 템플릿 3: `.claude/settings.json`

아래를 `.claude/settings.json` 으로 복사. **수정 불필요 — 그대로 사용.**

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/scripts/check.sh"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(.claude/scripts/*.sh:*)"
    ]
  }
}
```

---

## 8. 테스트 당일 세팅 명령 (복사해서 실행)

```bash
# 0. git repo 가 없는 경우만 (check.sh 가 git rev-parse 사용)
# git init

# 1. 템플릿 복사 (사전에 ~/harness-templates/ 에 준비한 것)
cp ~/harness-templates/CLAUDE.md ./CLAUDE.md
mkdir -p .claude/scripts
cp ~/harness-templates/.claude/scripts/check.sh .claude/scripts/check.sh
cp ~/harness-templates/.claude/settings.json .claude/settings.json
chmod +x .claude/scripts/check.sh

# 2. Claude Code 실행 (이 디렉토리에서)
# → settings.json 이 자동 로드되어 hook 활성화
# → 이후 모든 Write/Edit 에 check.sh 의 FAST 규칙(grep)이 자동 실행
```

---

## 9. 실전 적용 예시

### 예시 A: Go DDD 백엔드

**문제**: "주문 서비스를 DDD 로 구현하라"

**CLAUDE.md 에 채울 제약:**
```
## 금지 패턴
| F-1 | import "database/sql" in domain/ | internal/*/domain/ | 도메인 순수성 |
| F-2 | float64 in domain/ | internal/*/domain/ | 금융 정밀도 |
| F-3 | order/ imports balance/ directly | internal/order/domain/ | BC 분리 |

## 필수 패턴
| R-1 | Repository interface in domain/ | internal/*/domain/repository.go | DDD |
```

**check.sh 에 추가할 규칙:**
```bash
check_F1() {
    local hits=$(grep -rn '"database/sql"' internal/*/domain/ 2>/dev/null || true)
    [[ -n "$hits" ]] && { echo "FAIL [F-1]: domain imports database/sql"; FAIL=1; } || echo "PASS [F-1]"
}
check_F3() {
    local hits=$(grep -rn 'internal/balance' internal/order/domain/ 2>/dev/null || true)
    [[ -n "$hits" ]] && { echo "FAIL [F-3]: order domain imports balance"; FAIL=1; } || echo "PASS [F-3]"
}
```

### 예시 B: React 프론트엔드

**문제**: "컴포넌트 라이브러리를 구현하라"

**CLAUDE.md 에 채울 제약:**
```
## 금지 패턴
| F-1 | any in .ts/.tsx files | src/ | 타입 안전성 |
| F-2 | console.log | src/components/ | 프로덕션 코드에 디버그 로그 금지 |

## 필수 패턴
| R-1 | export default in every component | src/components/*.tsx | 컴포넌트 export |
| R-2 | data-testid in interactive elements | src/components/*.tsx | 테스트 가능성 |
```

**check.sh 에 추가할 규칙:**
```bash
check_F1() {
    local hits=$(grep -rnE '\bany\b' src/**/*.{ts,tsx} 2>/dev/null | grep -v node_modules || true)
    [[ -n "$hits" ]] && { echo "FAIL [F-1]: 'any' type found"; echo "$hits" | head -3; FAIL=1; } || echo "PASS [F-1]"
}
check_R2() {
    local missing=$(find src/components -name '*.tsx' -exec grep -L 'data-testid' {} \; 2>/dev/null)
    [[ -n "$missing" ]] && { echo "FAIL [R-2]: missing data-testid in:"; echo "$missing"; FAIL=1; } || echo "PASS [R-2]"
}
```

### 예시 C: Python FastAPI

**문제**: "REST API 를 구현하라"

**CLAUDE.md 에 채울 제약:**
```
## 금지 패턴
| F-1 | import sqlalchemy in domain/ | domain/ | 도메인 순수성 |
| F-2 | except Exception: (bare except) | src/ | 구체적 예외 처리 |

## 필수 패턴
| R-1 | Pydantic BaseModel for all DTOs | api/schemas/ | 타입 검증 |
| R-2 | async def in all route handlers | api/routes/ | 비동기 |
```

**check.sh 에 추가할 규칙:**
```bash
check_F1() {
    local hits=$(grep -rn 'import sqlalchemy\|from sqlalchemy' domain/ 2>/dev/null || true)
    [[ -n "$hits" ]] && { echo "FAIL [F-1]: domain imports sqlalchemy"; FAIL=1; } || echo "PASS [F-1]"
}
check_F2() {
    local hits=$(grep -rnE 'except\s+Exception\s*:' src/ 2>/dev/null || true)
    [[ -n "$hits" ]] && { echo "FAIL [F-2]: bare except found"; FAIL=1; } || echo "PASS [F-2]"
}
```

---

## 10. MVH → Full Harness 확장 로드맵

테스트가 끝나고 프로젝트로 발전시킬 때:

| 단계 | 추가 | 효과 |
|---|---|---|
| **MVH** (지금) | CLAUDE.md + check.sh + settings.json | 매 편집 자동 검증 |
| **+State** | `.claude/harness/state.json` + `next-task.sh` | task DAG + 자동 선택 + 상태 전이 |
| **+Fix** | `docs/04_Fix.md` + `last-failure.json` | reason → recipe 자동 주입 |
| **+Escalation** | failure_streak 카운터 + blocked 상태 전이 | 동일 reason N회 연속 실패 → blocked + 사람 개입 |
| **+Contracts** | `docs/01-03` 분리 (Plan/Code/Hook) | 관심사 분리, 대규모 프로젝트 |

Full harness 의 참조 구현: [이 프로젝트의 `.claude/harness/`](.claude/harness/) + [`docs/`](docs/)

---

## 11. 요약: 최소 3파일로 하네스 루프를 만든다

```
┌──────────────────────────────────────────────────────────────┐
│  CLAUDE.md         → 제약 정의 + 수정 레시피  (Plan + Fix)  │
│  check.sh          → grep 기반 자동 검증      (Hook)        │
│  settings.json     → 매 편집마다 hook 발화     (배선)       │
│                                                              │
│  사용자 프롬프트 → Claude 편집 → check.sh 자동 → 위반 시    │
│  Claude 가 출력을 보고 즉시 수정 → 반복                     │
│                                                              │
│  이것이 "Plan → Code → Hook → Fix" 루프의 최소 형태.        │
└──────────────────────────────────────────────────────────────┘
```
