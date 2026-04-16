#!/usr/bin/env bash
# next-task.sh — UserPromptSubmit hook target.
#
# Auto-bootstraps state.json if absent, then:
#   (1) if current_task is set   → re-emit context for that task
#   (2) else pick first eligible → transition pending→in_progress → emit context
#   (3) else HALT (blocked) or DONE (all done)
#
# Emits a <harness-context> block to stdout (consumed by the agent via
# UserPromptSubmit context injection).
#
# Usage:
#   next-task.sh                 # normal (used by hook)
#   next-task.sh --peek          # do not transition; just show what would be picked
#
# Exit:
#   0 normal
#   1 state corrupted / unrecoverable
#   2 infra error (from common.sh preflight)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

PEEK=0
if [[ "${1:-}" == "--peek" ]]; then PEEK=1; fi

# -- bootstrap --------------------------------------------------------------
if ! harness_state_exists; then
    mkdir -p "$HARNESS_ROOT"
    harness_state_init
    harness_log "state.json bootstrapped ($(jq '.tasks | length' "$HARNESS_STATE") tasks)"
fi

PLAN_JSON=$(harness_plan_json)
STATE=$(harness_state_read)

# plan drift — soft warn (reconcile-plan.sh handles; not fatal)
state_sha=$(jq -r '.plan_sha256 // ""' <<<"$STATE")
plan_sha=$(harness_plan_sha256)
if [[ "$state_sha" != "$plan_sha" ]]; then
    harness_log "plan drift: state=$state_sha current=$plan_sha (run reconcile-plan.sh)"
fi

current=$(jq -r '.current_task // empty' <<<"$STATE")

# -- helper: emit context for a given task id --------------------------------
emit_context() {
    local id="$1"
    local task_json state_json
    task_json=$(jq --arg id "$id" '.tasks[] | select(.id == $id)' <<<"$PLAN_JSON")
    state_json=$(jq --arg id "$id" '.tasks[$id]' <<<"$STATE")
    if [[ -z "$task_json" ]]; then
        printf 'HARNESS_INFRA_ERROR reason=task_not_in_plan task=%s\n' "$id" >&2
        return 1
    fi

    local title st attempts
    title=$(jq -r '.title' <<<"$task_json")
    st=$(jq -r '.state' <<<"$state_json")
    attempts=$(jq -r '.attempts // 0' <<<"$state_json")

    # Render a compact context block. yaml-ish flat lines so the agent parses trivially.
    {
        echo "<harness-context>"
        echo "current_task: $id"
        echo "title: $title"
        echo "state: $st"
        echo "attempts: $attempts"

        # files: creates / modifies (may be absent)
        local creates modifies
        creates=$(jq -r '.files.creates // [] | join(", ")' <<<"$task_json")
        modifies=$(jq -r '.files.modifies // [] | join(", ")' <<<"$task_json")
        [[ -n "$creates" ]]   && echo "files_creates: $creates"
        [[ -n "$modifies" ]]  && echo "files_modifies: $modifies"

        # deps state (so agent can see upstream satisfaction)
        local deps_csv
        deps_csv=$(jq -r --argjson s "$STATE" '
            (.deps // [])
            | map("\(.)=\($s.tasks[.] .state // "?")")
            | join(", ")
        ' <<<"$task_json")
        [[ -n "$deps_csv" ]] && echo "deps: $deps_csv"

        # exit_criteria — flatten to one line per criterion
        echo "exit_criteria:"
        jq -r '
            (.exit_criteria // [])[] |
            "  - " + (
                [ "hook=" + .hook ]
                + (if .rules     then ["rules=" + (.rules     | join(","))] else [] end)
                + (if .packages  then ["packages=" + (.packages  | join(","))] else [] end)
                + (if .tests     then ["tests=" + (.tests     | join(","))] else [] end)
                | join(" ")
            )
        ' <<<"$task_json"

        # last failure (if any) — drive the Fix loop
        if [[ -f "$HARNESS_LAST_FAILURE" ]]; then
            echo "last_failure:"
            jq -r '
                "  reason: \(.reason)\n" +
                "  file: \(.file // "-")\(if .line then ":\(.line)" else "" end)\n" +
                "  msg: \(.msg // "")\n" +
                "  recipe: see docs/04_Fix.md §2 row \(.reason)"
            ' "$HARNESS_LAST_FAILURE"
        fi
        echo "</harness-context>"
    }
}

# -- helper: pick first eligible task ---------------------------------------
pick_eligible() {
    # PLAN_JSON preserves tasks: array order → topological intent
    jq -r --argjson s "$STATE" '
        .tasks[]
        | select(
            (($s.tasks[.id].state // "pending") == "pending")
            and ((.deps // []) | all($s.tasks[.].state == "done"))
          )
        | .id
    ' <<<"$PLAN_JSON" | head -1
}

has_blocked() {
    jq -e '.tasks | to_entries | any(.value.state == "blocked")' <<<"$STATE" >/dev/null
}
all_done() {
    jq -e '.tasks | to_entries | all(.value.state == "done")' <<<"$STATE" >/dev/null
}

# -- (1) resume in-progress --------------------------------------------------
if [[ -n "$current" ]]; then
    emit_context "$current"
    exit 0
fi

# -- (2) pick eligible -------------------------------------------------------
next=$(pick_eligible)
if [[ -n "$next" ]]; then
    if (( PEEK )); then
        echo "<harness-context>"
        echo "peek: $next"
        echo "</harness-context>"
        exit 0
    fi
    # transition pending → in_progress
    STATE=$(jq --arg id "$next" --arg now "$(harness_now)" '
        .current_task = $id
        | .tasks[$id].state = "in_progress"
        | .tasks[$id].attempts = ((.tasks[$id].attempts // 0) + 1)
        | .tasks[$id].started_at = $now
        | .updated_at = $now
    ' <<<"$STATE")
    harness_state_write <<<"$STATE"
    harness_log "transition: $next pending→in_progress (attempts=$(jq -r --arg id "$next" '.tasks[$id].attempts' <<<"$STATE"))"
    emit_context "$next"
    exit 0
fi

# -- (3) HALT or DONE --------------------------------------------------------
if all_done; then
    echo "<harness-context>"
    echo "status: DONE"
    echo "message: all tasks completed"
    echo "</harness-context>"
    exit 0
fi
if has_blocked; then
    blocked=$(jq -r '.tasks | to_entries | map(select(.value.state == "blocked") | .key) | join(", ")' <<<"$STATE")
    echo "<harness-context>"
    echo "status: HALT"
    echo "reason: tasks_blocked"
    echo "blocked: $blocked"
    echo "action: run .claude/harness/scripts/unblock.sh <id> after addressing the root cause (see .claude/harness/blocked/)"
    echo "</harness-context>"
    exit 0
fi

# Pending tasks exist but none eligible → dep graph bug (validate-plan should have caught)
echo "HARNESS_PLAN_INVALID reason=no_eligible_but_pending_present" >&2
exit 1
