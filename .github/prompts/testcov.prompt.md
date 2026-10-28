---
description: "Run the test suite, fix the failures, then top up the coverage of the code this change touches"
---

<!-- Keep this prompt in sync with the Testing Procedures section of AGENTS.md. -->

# Test and coverage pass

1. Run `go test ./...` and fix every failure; repeat until the suite is green.
2. Measure the coverage with `task test:cover` (writes `coverage.out` and `coverage.html`) or
   `go test -cover ./...`.
3. For every package this change touches that sits below the **70% per-package target**, add tests
   for the new and least-covered code, prioritising error and edge-case paths.
4. Hot paths (request handling, scheduler loops, persistence) and security-relevant logic need
   **100% of their new branches** covered.
5. Iterate until the touched packages are at or above the target, then report the per-package
   numbers and name any package still below it.

The target is recommended and never a CI gate: never delete or weaken a test to reach a number.
See `AGENTS.md` (Testing Procedures) for the authoritative rules.
