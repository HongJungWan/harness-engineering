#!/usr/bin/env bash
# validate-plan.sh — PreToolUse hook target for edits to docs/01_Plan.md.
#
# Validates the task DAG structural invariants documented in
# docs/01_Plan.md §1 and §3:
#   - id uniqueness
#   - deps reference known ids
#   - DAG acyclic
#   - rules ∈ known rule set (02_Code.md §2)
#   - hooks ∈ known hook set (03_Hook.md §1)
#
# Usage:
#   validate-plan.sh                     # validate current docs/01_Plan.md
#   validate-plan.sh <file-being-edited> # if file != docs/01_Plan.md, exit 0
#   validate-plan.sh --all               # force validation
#
# Exit:
#   0 on PASS
#   1 on violation (emits HARNESS_PLAN_INVALID reason=<detail>)
#   2 on infra error (from common.sh preflight)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

# Only validate when the edit target is Plan.md (or when invoked explicitly).
target="${1:-}"
if [[ -n "$target" && "$target" != "--all" ]]; then
    # strip possible absolute prefix to compare tail
    case "$target" in
        *docs/01_Plan.md) : ;;
        *) exit 0 ;;
    esac
fi

fail() {
    printf 'HARNESS_PLAN_INVALID reason=%s\n' "$1"
    exit 1
}

# --- load plan --------------------------------------------------------------
if ! PLAN_JSON=$(harness_plan_json 2>/dev/null); then
    fail "yaml_block_missing_or_malformed"
fi

# --- structural checks -------------------------------------------------------
jq -e '.tasks | type == "array"' <<<"$PLAN_JSON" >/dev/null \
    || fail "tasks_not_array"

if jq -e '.tasks[] | select((.id | type) != "string" or (.title | type) != "string")' \
        <<<"$PLAN_JSON" >/dev/null; then
    fail "task_missing_id_or_title"
fi

# unique ids
dup=$(jq -r '
    .tasks
    | group_by(.id)
    | map(select(length > 1) | .[0].id)
    | .[]?
' <<<"$PLAN_JSON" | head -1)
if [[ -n "$dup" ]]; then
    fail "duplicate_id:$dup"
fi

# deps exist
missing_dep=$(jq -r '
    (.tasks | map(.id)) as $ids
    | .tasks[]
    | .deps[]? as $d
    | select($ids | index($d) | not)
    | "\(.id)->\($d)"
' <<<"$PLAN_JSON" | head -1)
if [[ -n "$missing_dep" ]]; then
    fail "missing_dep:$missing_dep"
fi

# DAG acyclic — offload DFS to lib (heredoc + herestring don't cooperate)
cycle=$(python3 "$HARNESS_LIB/dfs-cycle.py" <<<"$PLAN_JSON")
if [[ -n "$cycle" ]]; then
    fail "cycle:$cycle"
fi

# known rules (must match 02_Code.md §2)
valid_rules="DDD-1 DDD-2 DDD-4 MSA-1 MSA-3 EDA-1 EDA-2 EDA-3 RELAY-1 RELAY-5 FACADE-1 PERF-5"
invalid_rule=$(jq -r --arg ok "$valid_rules" '
    ($ok | split(" ")) as $valid
    | .tasks[]
    | .exit_criteria[]?
    | (.rules // [])[]
    | select($valid | index(.) | not)
' <<<"$PLAN_JSON" | head -1)
if [[ -n "$invalid_rule" ]]; then
    fail "unknown_rule:$invalid_rule"
fi

# known hooks (must match 03_Hook.md — any hook name used in exit_criteria)
valid_hooks="arch-check go-build go-test go-vet go-lint"
invalid_hook=$(jq -r --arg ok "$valid_hooks" '
    ($ok | split(" ")) as $valid
    | .tasks[]
    | .exit_criteria[]?
    | .hook
    | select(. != null and ($valid | index(.) | not))
' <<<"$PLAN_JSON" | head -1)
if [[ -n "$invalid_hook" ]]; then
    fail "unknown_hook:$invalid_hook"
fi

printf 'HARNESS_HOOK_PASS name=validate-plan\n'
harness_log "validate-plan PASS ($(jq '.tasks | length' <<<"$PLAN_JSON") tasks)"
exit 0
