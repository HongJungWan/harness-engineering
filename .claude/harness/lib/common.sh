#!/usr/bin/env bash
# shellcheck shell=bash
# Common helpers for harness scripts. SOURCE this file, don't execute it.
#
# Exports:
#   HARNESS_ROOT             — .claude/harness absolute path
#   HARNESS_STATE            — state.json path
#   HARNESS_LAST_FAILURE     — last-failure.json path
#   HARNESS_LOGS             — logs directory
#   HARNESS_LIB              — lib directory
#   PLAN_MD                  — docs/01_Plan.md absolute path
#   REPO_ROOT                — git toplevel
#
# Helpers:
#   harness_plan_json        — stdout: Plan.md yaml fence as JSON
#   harness_plan_sha256      — stdout: sha256 of the raw yaml
#   harness_state_exists     — return 0 if state.json present
#   harness_state_read       — stdout: state.json content
#   harness_state_write      — stdin → state.json (atomic)
#   harness_state_init       — bootstrap state.json from current Plan
#   harness_log              — append line to logs/hook-<date>.log (stderr sink)

set -euo pipefail

# -- paths -------------------------------------------------------------------
if ! REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null); then
    printf 'HARNESS_INFRA_ERROR missing=git_repo\n' >&2
    exit 2
fi
export REPO_ROOT

HARNESS_ROOT="${HARNESS_ROOT:-$REPO_ROOT/.claude/harness}"
HARNESS_LIB="${HARNESS_LIB:-$HARNESS_ROOT/lib}"
HARNESS_STATE="${HARNESS_STATE:-$HARNESS_ROOT/state.json}"
HARNESS_LAST_FAILURE="${HARNESS_LAST_FAILURE:-$HARNESS_ROOT/last-failure.json}"
HARNESS_LOGS="${HARNESS_LOGS:-$HARNESS_ROOT/logs}"
PLAN_MD="${PLAN_MD:-$REPO_ROOT/docs/01_Plan.md}"

export HARNESS_ROOT HARNESS_LIB HARNESS_STATE HARNESS_LAST_FAILURE HARNESS_LOGS PLAN_MD

# -- preflight ---------------------------------------------------------------
_harness_preflight() {
    local missing=()
    command -v python3 >/dev/null 2>&1 || missing+=("python3")
    command -v jq       >/dev/null 2>&1 || missing+=("jq")
    if command -v python3 >/dev/null 2>&1; then
        python3 -c 'import yaml' 2>/dev/null || missing+=("python3 yaml (PyYAML)")
    fi
    if (( ${#missing[@]} > 0 )); then
        printf 'HARNESS_INFRA_ERROR missing_tools=%s\n' "${missing[*]}" >&2
        printf 'install:\n  brew install jq\n  pip3 install pyyaml\n' >&2
        exit 2
    fi
}
_harness_preflight

# -- plan --------------------------------------------------------------------
harness_plan_json() {
    if [[ ! -f "$PLAN_MD" ]]; then
        printf 'HARNESS_INFRA_ERROR missing=%s\n' "$PLAN_MD" >&2
        return 2
    fi
    python3 "$HARNESS_LIB/plan-to-json.py" "$PLAN_MD"
}

harness_plan_sha256() {
    # hash only the raw yaml (not the rest of Plan.md) so prose edits don't invalidate state
    awk '/^```yaml$/{flag=1;next} /^```$/{if(flag)exit} flag' "$PLAN_MD" \
        | shasum -a 256 | awk '{print $1}'
}

# -- state.json --------------------------------------------------------------
harness_state_exists() { [[ -f "$HARNESS_STATE" ]]; }

harness_state_read() { cat "$HARNESS_STATE"; }

harness_state_write() {
    # atomic: write temp, rename
    mkdir -p "$(dirname "$HARNESS_STATE")"
    local tmp="${HARNESS_STATE}.tmp.$$"
    cat > "$tmp"
    mv "$tmp" "$HARNESS_STATE"
}

harness_state_init() {
    local plan_json now plan_sha tasks_obj
    plan_json=$(harness_plan_json)
    now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    plan_sha=$(harness_plan_sha256)
    tasks_obj=$(jq '
        .tasks
        | map({ (.id): { state: "pending", attempts: 0 } })
        | add // {}
    ' <<<"$plan_json")
    jq -n \
        --arg sha "$plan_sha" \
        --arg now "$now" \
        --argjson tasks "$tasks_obj" '{
            version: 1,
            plan_sha256: $sha,
            current_task: null,
            tasks: $tasks,
            updated_at: $now
        }' | harness_state_write
}

# -- logging -----------------------------------------------------------------
harness_log() {
    mkdir -p "$HARNESS_LOGS"
    local day
    day=$(date -u +%Y-%m-%d)
    printf '[%s] %s\n' "$(date -u +%FT%TZ)" "$*" >> "$HARNESS_LOGS/hook-$day.log"
}

# -- misc --------------------------------------------------------------------
harness_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
