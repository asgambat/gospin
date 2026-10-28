# Plan Template

## Implementation Plan

A plan is the working document for a change that spans more than one sitting or more than one
package. Keep it short enough to reread before every session; the code and the ADRs carry the rest.

## 1. Title

One line, max 120 characters, naming the outcome.

## 2. Context and Goal

Why the change is needed, what exists today, and what "done" looks like. Link the ADR or the issue
that motivates it instead of restating it.

## 3. Status

- **State**: `proposed`, `in-progress`, `blocked`, `done` or `dropped`.
- **Last updated**: `YYYY-MM-DD`.
- **Blockers**: what is waiting on someone else, or `none`.

## 4. Scope

- **In**: the files, endpoints, configuration keys or documents the change touches.
- **Out**: what is deliberately not touched, so the change stays reviewable.

## 5. Task List

One row per coherent step, in the order they will be done. `Done` is updated as the work proceeds.

| ID    | Task                                       | Depends on | Complexity | Done |
| ----- | ------------------------------------------ | ---------- | ---------- | ---- |
| T-001 |                                            | –          | S          | no   |

Complexity: `XS`, `S`, `M`, `L`, `XL`. No time estimates.

## 6. Verification

What has to pass before the plan is closed:

- the `task check` gate (fmt, vet, staticcheck, golangci-lint, tests) and `task test:race` when
  goroutines are involved;
- the tests added or updated, named explicitly;
- the behaviour exercised the way a user does it (HTTP request, CLI invocation, page load);
- the documentation updated in the same change.

## 7. Risks and Decisions

- Risks with the mitigation that keeps them small.
- Decisions taken while executing the plan that deserve an ADR instead of a paragraph here.

## 8. References

- Related ADRs, PRDs, `docs/ARCHITECTURE.md` sections and the workflows that will run the change.
