#!/usr/bin/env python3
"""
Extract the task-list yaml fenced block from docs/01_Plan.md and emit JSON.

Selection rule: find every ```yaml fenced block, pick the one whose body's
first non-blank line is the sentinel `# harness:plan-tasks`.  Other ```yaml
blocks in the doc (schema examples, etc.) are ignored.

Exit codes:
  0 — JSON on stdout
  1 — sentinel block not found
  2 — YAML parse error or missing PyYAML
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

SENTINEL = "# harness:plan-tasks"


def _first_nonblank(s: str) -> str:
    for line in s.splitlines():
        if line.strip():
            return line.strip()
    return ""


def main() -> int:
    if len(sys.argv) > 1:
        text = Path(sys.argv[1]).read_text(encoding="utf-8")
    else:
        text = sys.stdin.read()

    blocks = re.findall(r"```yaml\n(.*?)\n```", text, re.DOTALL)
    if not blocks:
        sys.stderr.write("plan-to-json: no ```yaml fenced blocks found\n")
        return 1

    selected = None
    for body in blocks:
        if _first_nonblank(body) == SENTINEL:
            selected = body
            break

    if selected is None:
        sys.stderr.write(
            f"plan-to-json: no ```yaml block starts with sentinel '{SENTINEL}'\n"
        )
        return 1

    try:
        import yaml  # PyYAML
    except ImportError:
        sys.stderr.write(
            "plan-to-json: PyYAML missing. install: pip3 install pyyaml\n"
        )
        return 2

    try:
        doc = yaml.safe_load(selected)
    except yaml.YAMLError as e:  # type: ignore[attr-defined]
        sys.stderr.write(f"plan-to-json: YAML parse error: {e}\n")
        return 2

    json.dump(doc, sys.stdout, ensure_ascii=False, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
