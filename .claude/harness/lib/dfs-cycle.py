#!/usr/bin/env python3
"""
Read plan JSON from stdin, print the first cycle (if any) as 'a->b->c->a'.
Prints nothing and exits 0 when the graph is acyclic.
"""
import json
import sys


def main() -> int:
    data = json.load(sys.stdin)
    graph = {t["id"]: (t.get("deps") or []) for t in data.get("tasks", [])}
    color = {k: 0 for k in graph}  # 0=white, 1=gray, 2=black
    stack: list[str] = []

    def visit(n: str) -> None:
        if color.get(n, 0) == 1:
            print("->".join(stack + [n]))
            sys.exit(0)
        if color.get(n, 0) == 2:
            return
        color[n] = 1
        stack.append(n)
        for d in graph.get(n, []):
            if d in graph:
                visit(d)
        stack.pop()
        color[n] = 2

    for node in list(graph):
        if color[node] == 0:
            visit(node)
    return 0


if __name__ == "__main__":
    sys.exit(main())
