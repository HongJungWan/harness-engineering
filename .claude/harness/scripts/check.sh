#!/usr/bin/env bash
# check.sh — PostToolUse hook target (on Write|Edit|MultiEdit).
#
# Runs applicable architecture rules (12 ID'd in 02_Code.md §2) plus optional
# go-build / go-test / go-vet. Each rule is a bash function. The file glob
# heuristics decide which rules run when invoked with one or more files;
# --all forces every rule.
#
# Output (03_Hook.md §3 grammar):
#   HARNESS_HOOK_PASS name=<RULE-ID>
#   HARNESS_HOOK_FAIL name=<RULE-ID> reason=<reason-key> [file=<path>[:<line>]] [msg="..."]
#
# Side effects:
#   On any FAIL → writes .claude/harness/last-failure.json (first FAIL only).
#   On all PASS → removes .claude/harness/last-failure.json.
#
# Usage:
#   check.sh                            # same as --all if no files given
#   check.sh --all
#   check.sh <file> [<file>...]
#   check.sh --only DDD-1 [<file>]      # single rule (ID case-insensitive)
#   check.sh --with-build               # include GO-BUILD
#   check.sh --with-test                # include GO-TEST (and GO-BUILD)
#
# Exit:
#   0 — all invoked rules PASS
#   1 — ≥1 rule FAIL
#   2 — infra error

set -uo pipefail   # not -e: individual rule failures must not abort dispatch

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

cd "$REPO_ROOT"

# --- args -------------------------------------------------------------------
ONLY=""
ALL=0
WITH_BUILD=0
WITH_TEST=0
FILES=()
while (( $# )); do
    case "$1" in
        --all)         ALL=1; shift ;;
        --only)        ONLY=$(printf '%s' "$2" | tr '[:lower:]' '[:upper:]'); shift 2 ;;
        --with-build)  WITH_BUILD=1; shift ;;
        --with-test)   WITH_TEST=1; WITH_BUILD=1; shift ;;
        --) shift; while (( $# )); do FILES+=("$1"); shift; done ;;
        -*) printf 'HARNESS_INFRA_ERROR reason=unknown_flag flag=%s\n' "$1" >&2; exit 2 ;;
        *)  FILES+=("$1"); shift ;;
    esac
done
# normalize absolute paths → repo-relative
for i in "${!FILES[@]}"; do
    FILES[$i]="${FILES[$i]#$REPO_ROOT/}"
done
if (( ${#FILES[@]} == 0 )) && (( ALL == 0 )) && [[ -z "$ONLY" ]]; then
    ALL=1
fi

# --- output capture ---------------------------------------------------------
OUT_BUF=""
emit_pass() { local line; line="HARNESS_HOOK_PASS name=$1"; echo "$line"; OUT_BUF+="$line"$'\n'; }
emit_fail() {
    local name="$1" reason="$2" file="${3:-}" msg="${4:-}"
    local line="HARNESS_HOOK_FAIL name=$name reason=$reason"
    [[ -n "$file" ]] && line+=" file=$file"
    [[ -n "$msg"  ]] && line+=" msg=\"$msg\""
    echo "$line"
    OUT_BUF+="$line"$'\n'
}

# --- rule implementations ---------------------------------------------------
# each rule emits exactly one PASS line or ≥1 FAIL lines, never both.

_grep_domain_dirs=(internal/order/domain internal/balance/domain internal/shared/domain)

check_DDD_1() {
    local any=0
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        local file line
        file=${hit%%:*}; rest=${hit#*:}; line=${rest%%:*}
        emit_fail DDD-1 ddd-1 "$file:$line" "domain imports infrastructure"
        any=1
    done < <(grep -rEn '"[^"]*/infrastructure"' "${_grep_domain_dirs[@]}" 2>/dev/null | grep -v _test.go || true)
    (( any )) || emit_pass DDD-1
}

check_DDD_2() {
    local any=0
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        local file line
        file=${hit%%:*}; rest=${hit#*:}; line=${rest%%:*}
        emit_fail DDD-2 ddd-2 "$file:$line" "domain imports DB driver"
        any=1
    done < <(grep -rEn '"(database/sql|github.com/jmoiron/sqlx|github.com/go-sql-driver[^"]*)"' "${_grep_domain_dirs[@]}" 2>/dev/null | grep -v _test.go || true)
    (( any )) || emit_pass DDD-2
}

check_DDD_4() {
    local any=0
    for bc in order balance; do
        if [[ ! -f "internal/$bc/domain/repository.go" ]]; then
            emit_fail DDD-4 ddd-4 "internal/$bc/domain/repository.go" "missing repository interface in domain"
            any=1
        fi
        if ! compgen -G "internal/$bc/infrastructure/mysql_*_repo.go" >/dev/null; then
            emit_fail DDD-4 ddd-4 "internal/$bc/infrastructure/" "missing MySQL repo impl"
            any=1
        fi
    done
    (( any )) || emit_pass DDD-4
}

check_MSA_1() {
    local any=0
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        local file line
        file=${hit%%:*}; rest=${hit#*:}; line=${rest%%:*}
        emit_fail MSA-1 msa-1 "$file:$line" "order/domain imports balance"
        any=1
    done < <(grep -rn '"github.com/HongJungWan/harness-engineering/internal/balance' internal/order/domain 2>/dev/null | grep -v _test.go || true)
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        local file line
        file=${hit%%:*}; rest=${hit#*:}; line=${rest%%:*}
        emit_fail MSA-1 msa-1 "$file:$line" "balance/domain imports order"
        any=1
    done < <(grep -rn '"github.com/HongJungWan/harness-engineering/internal/order' internal/balance/domain 2>/dev/null | grep -v _test.go || true)
    (( any )) || emit_pass MSA-1
}

check_MSA_3() {
    # Heuristic: json.Marshal/NewEncoder on a symbol from the domain package.
    # "domain.X" appearing in the same marshal line is the simplest signal.
    local any=0
    local files=()
    while IFS= read -r g; do [[ -n "$g" ]] && files+=("$g"); done < <(compgen -G 'internal/*/presentation/*.go' || true)
    for f in "${files[@]}"; do
        [[ -z "$f" ]] && continue
        case "$f" in *_test.go) continue ;; esac
        while IFS=: read -r ln content; do
            [[ -z "$ln" ]] && continue
            # Allow if DTO/Response/Request in the line
            if [[ "$content" == *"Response"* || "$content" == *"Request"* || "$content" == *"DTO"* || "$content" == *"dto"* ]]; then
                continue
            fi
            # Only flag if we can see a `domain.` reference nearby (same line)
            if [[ "$content" == *"domain."* ]]; then
                emit_fail MSA-3 msa-3 "$f:$ln" "handler may expose domain entity"
                any=1
            fi
        done < <(grep -nE 'json\.(Marshal|NewEncoder)' "$f" 2>/dev/null || true)
    done
    (( any )) || emit_pass MSA-3
}

check_EDA_1() {
    local f="internal/order/presentation/kafka_consumer.go"
    if [[ ! -f "$f" ]]; then emit_pass EDA-1; return; fi
    if grep -qE '(DLQTopic|DLQProducer|publishToDLQ|dlq)' "$f"; then
        emit_pass EDA-1
    else
        emit_fail EDA-1 eda-1 "$f" "missing DLQ handling"
    fi
}

check_EDA_2() {
    local f="internal/order/presentation/kafka_consumer.go"
    if [[ ! -f "$f" ]]; then emit_pass EDA-2; return; fi
    if grep -q 'processed_events' "$f"; then
        emit_pass EDA-2
    else
        emit_fail EDA-2 eda-2 "$f" "missing processed_events idempotency"
    fi
}

check_EDA_3() {
    # For every line with "INSERT INTO outbox_events" in infra MySQL repos,
    # ensure a tx.Exec / tx.ExecContext / Tx.Exec appears within ±10 lines.
    local any=0
    local files=()
    while IFS= read -r g; do [[ -n "$g" ]] && files+=("$g"); done < <(compgen -G 'internal/*/infrastructure/mysql_*_repo.go' || true)
    for f in "${files[@]}"; do
        [[ -z "$f" ]] && continue
        local bad
        bad=$(awk '
            { lines[NR]=$0 }
            /INSERT INTO outbox_events/ { hits[NR]=1 }
            END {
                for (n in hits) {
                    ok=0
                    lo=n-10; if (lo<1) lo=1
                    hi=n+10
                    for (i=lo; i<=hi; i++) {
                        if (lines[i] ~ /(tx\.Exec|tx\.ExecContext|Tx\.Exec|Tx\.ExecContext)/) { ok=1; break }
                    }
                    if (!ok) print n
                }
            }
        ' "$f")
        while IFS= read -r l; do
            [[ -z "$l" ]] && continue
            emit_fail EDA-3 eda-3 "$f:$l" "outbox INSERT not within ±10 lines of tx.Exec"
            any=1
        done <<<"$bad"
    done
    (( any )) || emit_pass EDA-3
}

check_RELAY_1() {
    if grep -rq 'SKIP LOCKED' internal/outbox 2>/dev/null; then
        emit_pass RELAY-1
    else
        emit_fail RELAY-1 relay-1 "internal/outbox/" "relay missing SKIP LOCKED"
    fi
}

check_RELAY_5() {
    local f="internal/outbox/relay.go"
    if [[ ! -f "$f" ]]; then emit_pass RELAY-5; return; fi
    if grep -qE '(CountStuckEvents|detectStuckEvents|[sS]tuck)' "$f"; then
        emit_pass RELAY-5
    else
        emit_fail RELAY-5 relay-5 "$f" "relay missing stuck-event detection"
    fi
}

check_FACADE_1() {
    local f="internal/outbox/relay.go"
    if [[ ! -f "$f" ]]; then emit_pass FACADE-1; return; fi
    local hit
    hit=$(grep -n '"github.com/IBM/sarama"' "$f" 2>/dev/null || true)
    if [[ -n "$hit" ]]; then
        local line=${hit%%:*}
        emit_fail FACADE-1 facade-1 "$f:$line" "relay imports sarama directly (use EventProducer)"
    else
        emit_pass FACADE-1
    fi
}

check_PERF_5() {
    local any=0
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        local file line content
        file=${hit%%:*}; rest=${hit#*:}; line=${rest%%:*}; content=${rest#*:}
        # skip pure comments
        if [[ "$content" =~ ^[[:space:]]*// ]]; then continue; fi
        emit_fail PERF-5 perf-5 "$file:$line" "float in domain — use shopspring/decimal"
        any=1
    done < <(grep -rEn '\bfloat(32|64)\b' "${_grep_domain_dirs[@]}" 2>/dev/null | grep -v _test.go || true)
    (( any )) || emit_pass PERF-5
}

check_GO_BUILD() {
    local out rc
    out=$(go build ./... 2>&1); rc=$?
    if (( rc == 0 )); then
        emit_pass GO-BUILD
    else
        local first; first=$(echo "$out" | head -1 | tr -d '"' | tr '\n' ' ')
        emit_fail GO-BUILD go-build "" "$first"
    fi
}

check_GO_TEST() {
    local out rc
    out=$(go test -short -count=1 ./... 2>&1); rc=$?
    if (( rc == 0 )); then
        emit_pass GO-TEST
    else
        local summary; summary=$(echo "$out" | grep -E '^(FAIL|---\s+FAIL)' | head -1 | tr -d '"')
        [[ -z "$summary" ]] && summary=$(echo "$out" | tail -1 | tr -d '"')
        emit_fail GO-TEST go-test "" "$summary"
    fi
}

check_GO_VET() {
    local out rc
    out=$(go vet ./... 2>&1); rc=$?
    if (( rc == 0 )); then
        emit_pass GO-VET
    else
        local first; first=$(echo "$out" | head -1 | tr -d '"')
        emit_fail GO-VET go-vet "" "$first"
    fi
}

# --- dispatch ---------------------------------------------------------------
ALL_ARCH_RULES=(DDD-1 DDD-2 DDD-4 MSA-1 MSA-3 EDA-1 EDA-2 EDA-3 RELAY-1 RELAY-5 FACADE-1 PERF-5)

RULES_TO_RUN=()

# bash-3.2-compat: append if not already present
add_rule() {
    local r="$1" existing
    for existing in "${RULES_TO_RUN[@]:-}"; do
        [[ "$existing" == "$r" ]] && return
    done
    RULES_TO_RUN+=("$r")
}

if [[ -n "$ONLY" ]]; then
    RULES_TO_RUN=("$ONLY")
elif (( ALL )); then
    for r in "${ALL_ARCH_RULES[@]}"; do RULES_TO_RUN+=("$r"); done
    (( WITH_BUILD )) && RULES_TO_RUN+=(GO-BUILD)
    (( WITH_TEST ))  && RULES_TO_RUN+=(GO-TEST)
else
    # per-file glob selection
    for f in "${FILES[@]:-}"; do
        [[ -z "$f" ]] && continue
        case "$f" in
            internal/order/domain/*|internal/balance/domain/*|internal/shared/domain/*)
                add_rule DDD-1; add_rule DDD-2; add_rule PERF-5; add_rule DDD-4 ;;
        esac
        case "$f" in
            internal/order/domain/*)   add_rule MSA-1 ;;
            internal/balance/domain/*) add_rule MSA-1 ;;
        esac
        case "$f" in
            internal/*/infrastructure/mysql_*_repo.go) add_rule DDD-4; add_rule EDA-3 ;;
        esac
        case "$f" in
            internal/*/presentation/*.go) add_rule MSA-3 ;;
        esac
        case "$f" in
            internal/order/presentation/kafka_consumer.go) add_rule EDA-1; add_rule EDA-2 ;;
        esac
        case "$f" in
            internal/outbox/*) add_rule RELAY-1; add_rule RELAY-5; add_rule FACADE-1 ;;
        esac
    done
    (( WITH_BUILD )) && RULES_TO_RUN+=(GO-BUILD)
    (( WITH_TEST ))  && RULES_TO_RUN+=(GO-TEST)
fi

if (( ${#RULES_TO_RUN[@]} == 0 )); then
    # nothing applicable to the given file(s) → non-issue
    harness_log "check.sh: no applicable rules for files=[${FILES[*]:-}]"
    exit 0
fi

for r in "${RULES_TO_RUN[@]}"; do
    fn="check_${r//-/_}"
    if declare -f "$fn" >/dev/null 2>&1; then
        "$fn"
    else
        printf 'HARNESS_INFRA_ERROR reason=unknown_rule rule=%s\n' "$r" >&2
    fi
done

# --- aggregate → last-failure.json ------------------------------------------
first_fail=$(echo "$OUT_BUF" | grep -E '^HARNESS_HOOK_FAIL' | head -1 || true)
if [[ -n "$first_fail" ]]; then
    # parse: name=<id> reason=<key> [file=<p:l>] [msg="..."]
    name=$(  sed -nE 's/.*name=([^ ]+).*/\1/p'   <<<"$first_fail")
    reason=$(sed -nE 's/.*reason=([^ ]+).*/\1/p' <<<"$first_fail")
    fileln=$(sed -nE 's/.*file=([^ ]+).*/\1/p'   <<<"$first_fail")
    msg=$(   sed -nE 's/.*msg="([^"]*)".*/\1/p'  <<<"$first_fail")
    file="${fileln%%:*}"
    line="${fileln#*:}"
    [[ "$line" == "$file" ]] && line=""   # no :line

    now=$(harness_now)
    current_task=$(jq -r '.current_task // ""' "$HARNESS_STATE" 2>/dev/null || echo "")
    prior_attempts=$(jq -r --arg id "$current_task" '.tasks[$id].attempts // 0' "$HARNESS_STATE" 2>/dev/null || echo 0)

    mkdir -p "$(dirname "$HARNESS_LAST_FAILURE")"
    jq -n \
        --arg ts "$now" \
        --arg task "$current_task" \
        --arg rule "$name" \
        --arg reason "$reason" \
        --arg file "$file" \
        --arg line "$line" \
        --arg msg "$msg" \
        --argjson prior "$prior_attempts" '{
            timestamp: $ts,
            task_id: $task,
            rule_id: $rule,
            reason: $reason,
            file: (if $file == "" then null else $file end),
            line: (if $line == "" then null else ($line | tonumber? // null) end),
            msg: $msg,
            attempts_before: $prior
        }' > "$HARNESS_LAST_FAILURE.tmp.$$"
    mv "$HARNESS_LAST_FAILURE.tmp.$$" "$HARNESS_LAST_FAILURE"

    harness_log "check FAIL: rule=$name reason=$reason file=$file:$line"
    exit 1
fi

# all PASS — clear stale last-failure
[[ -f "$HARNESS_LAST_FAILURE" ]] && rm -f "$HARNESS_LAST_FAILURE"
harness_log "check PASS: rules=[${RULES_TO_RUN[*]}]"
exit 0
