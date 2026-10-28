# PRD Template

## Product Requirements Document (PRD)

A PRD is only written when a feature needs agreement on *what* to build before anyone decides *how*.
For a feature that is already clear, an ADR or a plan is the right document.

### 1. Overview

- **Problem Statement:** What problem are we solving, and for whom?
- **Value Proposition:** What changes for the user once this ships?

### 2. Goals and Non-Goals

- **Goals:** The outcomes this release must reach.
- **Non-Goals:** What is deliberately left out, so scope cannot creep back in.

### 3. Requirements

Functional requirements, each with a priority (`Must`, `Should`, `Could`) and the user-visible
behaviour that proves it:

| Priority | Requirement | Acceptance |
| -------- | ----------- | ---------- |
| Must     |             |            |

Non-functional requirements: performance, resource use on the target device (a Raspberry Pi is the
reference host), security, and compatibility with the existing API and configuration keys.

### 4. Success Criteria

How the maintainer will know the feature works: the observable behaviour, the test that covers it and
any documentation that has to exist for it to be usable.

### 5. References

- The ADRs the feature depends on or will produce.
- Related sections of `README.md`, `docs/ARCHITECTURE.md` and `docs/EXAMPLES.md`.
