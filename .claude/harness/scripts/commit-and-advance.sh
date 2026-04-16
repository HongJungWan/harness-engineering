#!/usr/bin/env bash
# commit-and-advance.sh — Stop hook.
#
# Closes the current task: runs check.sh with flags derived from the task's
# exit_criteria, and on all-PASS:
#   - updates state.json (in_progress → done, clears failure_streak)
#   - commits with the §5 trailer (docs/01_Plan.md)
#   - emits HARNESS_STATE_TRANSITION
# On FAIL:
#   - increments failure_streak (reset to 1 when reason changes)
#   - at count ≥ 3 → dumps blocked/<id>.md, transitions to blocked,
#     emits HARNESS_ESCALATION
#   - otherwise emits HARNESS_TURN_INCOMPLETE (WIP left uncommitted)
#
# Option A (infra-awareness): if the task demands go-test but no Kafka-like
# container is running, emit HARNESS_INFRA_MISSING and exit 2 WITHOUT touching
# the failure streak. Infra absence is not a code failure.
#
# Exit: 0 success, 1 fail (incomplete or escalated), 2 infra missing, 2 from
# common.sh preflight.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

cd "$REPO_ROOT"

# --- 0. no-op if nothing is in flight ---------------------------------------
if ! harness_state_exists; then exit 0; fi
STATE=$(harness_state_read)
TASK_ID=$(jq -r '.current_task // empty' <<<"$STATE")
[[ -z "$TASK_ID" ]] && exit 0

# --- 1. load task + derive flags --------------------------------------------
PLAN=$(harness_plan_json)
TASK=$(jq --arg id "$TASK_ID" '.tasks[] | select(.id == $id)' <<<"$PLAN")
if [[ -z "$TASK" ]]; then
    printf 'HARNESS_INFRA_ERROR reason=task_not_in_plan task=%s\n' "$TASK_ID" >&2
    exit 2
fi

HOOKS_LISTED=$(jq -r '.exit_criteria[]?.hook // empty' <<<"$TASK")
NEEDS_BUILD=0
NEEDS_TEST=0
if grep -q '^go-build$' <<<"$HOOKS_LISTED"; then NEEDS_BUILD=1; fi
if grep -q '^go-test$'  <<<"$HOOKS_LISTED"; then NEEDS_TEST=1; fi

# --- 2. option A: infra-aware test gating -----------------------------------
if (( NEEDS_TEST )); then
    if ! docker ps --format '{{.Image}}' 2>/dev/null | grep -qiE 'kafka|redpanda'; then
        printf 'HARNESS_INFRA_MISSING reason=docker_kafka_down task=%s\n' "$TASK_ID"
        printf 'hint: make docker-up && edit any file to re-trigger Stop\n'
        harness_log "infra missing (kafka) for $TASK_ID — skipped without streak"
        exit 2
    fi
fi

# --- 3. run check.sh --------------------------------------------------------
FLAGS=(--all)
(( NEEDS_BUILD )) && FLAGS+=(--with-build)
(( NEEDS_TEST  )) && FLAGS+=(--with-test)

OUTPUT=$("$SCRIPT_DIR/check.sh" "${FLAGS[@]}" 2>&1)
RC=$?
printf '%s\n' "$OUTPUT"

# --- helpers ----------------------------------------------------------------
_commit_type() {
    case "$1" in
        TEST-*) echo test ;;
        DOCS-*) echo docs ;;
        *)      echo feat ;;
    esac
}

_dump_blocked() {
    local id="$1" reason="$2" count="$3" out="$4"
    local dir="$HARNESS_ROOT/blocked"
    local file="$dir/$id.md"
    mkdir -p "$dir"
    local title first_fail diff_out
    title=$(jq -r --arg id "$id" '.tasks[] | select(.id==$id) | .title' <<<"$PLAN")
    first_fail=$(grep -E '^HARNESS_HOOK_FAIL' <<<"$out" | head -1 || true)
    diff_out=$(git diff HEAD 2>&1 | head -200)
    {
        printf '# Blocked: %s\n\n' "$id"
        printf '**Title**: %s\n' "$title"
        printf '**Blocked at**: %s\n' "$(harness_now)"
        printf '**Failure streak**: `%s` × %d\n\n' "$reason" "$count"
        printf '## First failure\n\n```\n%s\n```\n\n' "$first_fail"
        printf '## check.sh tail\n\n```\n%s\n```\n\n' "$(tail -20 <<<"$out")"
        printf '## WIP diff (git diff HEAD, truncated)\n\n```diff\n%s\n```\n\n' "$diff_out"
        printf '## Recipe\n\nsee docs/04_Fix.md §2 row `%s`\n\n' "$reason"
        printf '## Unblock\n\n'
        printf '1. Resolve the root cause in code.\n'
        printf '2. Verify: `.claude/harness/scripts/check.sh --all` must PASS.\n'
        printf '3. Flip state:\n\n'
        printf '   ```bash\n'
        printf '   jq '\''.tasks["%s"].state = "pending" | del(.tasks["%s"].failure_streak) | .current_task = null'\'' \\\n' "$id" "$id"
        printf '     .claude/harness/state.json > .claude/harness/state.json.tmp \\\n'
        printf '     && mv .claude/harness/state.json.tmp .claude/harness/state.json\n'
        printf '   ```\n'
    } > "$file"
    harness_log "dumped blocked/$id.md"
}

# --- 4. dispatch ------------------------------------------------------------
NOW=$(harness_now)

if (( RC == 0 )); then
    # 4a. SUCCESS --- commit + transition
    HOOKS_PASSED=$(grep -E '^HARNESS_HOOK_PASS' <<<"$OUTPUT" \
        | awk '{for(i=1;i<=NF;i++) if($i ~ /^name=/){sub("name=","",$i); print tolower($i)}}' \
        | paste -sd, -)
    ATTEMPTS=$(jq -r --arg id "$TASK_ID" '.tasks[$id].attempts // 1' <<<"$STATE")
    CTYPE=$(_commit_type "$TASK_ID")
    TITLE=$(jq -r '.title' <<<"$TASK")

    # write state.json FIRST so `git add -A` picks it up in the same commit
    jq --arg id "$TASK_ID" --arg now "$NOW" '
        .current_task = null
      | .tasks[$id].state = "done"
      | .tasks[$id].completed_at = $now
      | del(.tasks[$id].failure_streak)
      | .updated_at = $now
    ' <<<"$STATE" | harness_state_write

    COMMIT_MSG="$CTYPE($TASK_ID): $TITLE

task_id: $TASK_ID
state: in_progress → done
hooks_passed: $HOOKS_PASSED
attempts: $ATTEMPTS"

    if ! git add -A >/dev/null 2>&1; then
        printf 'HARNESS_INFRA_ERROR reason=git_add_failed task=%s\n' "$TASK_ID" >&2
        exit 2
    fi
    if ! git commit -m "$COMMIT_MSG" >/dev/null 2>&1; then
        # possible cause: nothing to commit (shouldn't happen — state.json changed).
        # Fall back to --allow-empty so we never leave a done-in-state-but-no-commit gap.
        git commit --allow-empty -m "$COMMIT_MSG" >/dev/null 2>&1 || {
            printf 'HARNESS_INFRA_ERROR reason=git_commit_failed task=%s\n' "$TASK_ID" >&2
            exit 2
        }
    fi
    SHA=$(git rev-parse HEAD)
    printf 'HARNESS_STATE_TRANSITION task=%s from=in_progress to=done commit=%s\n' "$TASK_ID" "$SHA"
    harness_log "DONE $TASK_ID ($SHA)"
    exit 0
fi

# 4b. FAIL --- streak + maybe escalate
FIRST_FAIL=$(grep -E '^HARNESS_HOOK_FAIL' <<<"$OUTPUT" | head -1 || true)
REASON=$(printf '%s' "$FIRST_FAIL" | sed -nE 's/.*reason=([^ ]+).*/\1/p')
[[ -z "$REASON" ]] && REASON=unknown

PREV_REASON=$(jq -r --arg id "$TASK_ID" '.tasks[$id].failure_streak.reason // empty' <<<"$STATE")
PREV_COUNT=$(jq -r --arg id "$TASK_ID" '.tasks[$id].failure_streak.count  // 0'     <<<"$STATE")
if [[ "$PREV_REASON" == "$REASON" ]]; then
    NEW_COUNT=$((PREV_COUNT + 1))
else
    NEW_COUNT=1
fi

if (( NEW_COUNT >= 3 )); then
    _dump_blocked "$TASK_ID" "$REASON" "$NEW_COUNT" "$OUTPUT"
    jq --arg id "$TASK_ID" --arg reason "$REASON" --argjson n "$NEW_COUNT" --arg now "$NOW" '
        .current_task = null
      | .tasks[$id].state = "blocked"
      | .tasks[$id].failure_streak = { reason: $reason, count: $n }
      | .updated_at = $now
    ' <<<"$STATE" | harness_state_write
    printf 'HARNESS_ESCALATION task=%s reason=%s attempts=%d\n' "$TASK_ID" "$REASON" "$NEW_COUNT"
    harness_log "ESCALATE $TASK_ID reason=$REASON count=$NEW_COUNT"
else
    jq --arg id "$TASK_ID" --arg reason "$REASON" --argjson n "$NEW_COUNT" --arg now "$NOW" '
        .tasks[$id].failure_streak = { reason: $reason, count: $n }
      | .updated_at = $now
    ' <<<"$STATE" | harness_state_write
    printf 'HARNESS_TURN_INCOMPLETE task=%s reason=%s streak=%d\n' "$TASK_ID" "$REASON" "$NEW_COUNT"
    harness_log "incomplete $TASK_ID reason=$REASON streak=$NEW_COUNT"
fi

# WIP is intentionally left uncommitted for the next turn to re-examine.
exit 1
