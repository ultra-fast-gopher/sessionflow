#!/usr/bin/env python3
"""Run every isolated integration module with bounded, readable output."""
from __future__ import annotations

import concurrent.futures
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
ALL_MODULES = sorted(path.parent for path in (ROOT / "integration").rglob("go.mod"))
SELECTORS = set(sys.argv[1:])
MODULES = [module for module in ALL_MODULES if not SELECTORS or str(module.relative_to(ROOT)) in SELECTORS]


def run(module: pathlib.Path) -> tuple[pathlib.Path, list[tuple[str, int, str]]]:
    steps = []
    for label, cmd in (("test", ["go", "test", "-race", "-timeout", "120s", "./..."]), ("vet", ["go", "vet", "./..."])):
        try:
            result = subprocess.run(cmd, cwd=module, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=300)
            steps.append((label, result.returncode, result.stdout))
        except subprocess.TimeoutExpired as error:
            output = error.stdout or ""
            if isinstance(output, bytes):
                output = output.decode(errors="replace")
            steps.append((label, 124, f"command timed out after 300s\n{output}"))
        except OSError as error:
            steps.append((label, 127, f"could not run command: {error}"))
        if steps[-1][1]:
            break
    return module, steps


def main() -> int:
    if not MODULES:
        print("no matching nested integration modules found", file=sys.stderr)
        return 1
    failures = []
    print(f"checking {len(MODULES)} integration modules (2 workers)", flush=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(run, module) for module in MODULES]
        for future in concurrent.futures.as_completed(futures):
            module, steps = future.result()
            failed = any(code for _, code, _ in steps)
            print(f"{'FAIL' if failed else 'PASS'} {module.relative_to(ROOT)}", flush=True)
            if failed:
                failures.append((module, steps))
    for module, steps in sorted(failures, key=lambda item: str(item[0])):
        for label, code, output in steps:
            if code:
                print(f"\n--- {module.relative_to(ROOT)}: {label} (exit {code}) ---", file=sys.stderr)
                print(output, file=sys.stderr, end="" if output.endswith("\n") else "\n")
    print(f"{len(MODULES) - len(failures)}/{len(MODULES)} integration modules passed", flush=True)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
