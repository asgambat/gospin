# GitHub Copilot Instructions

<!--
==============================================================================
COPILOT INSTRUCTIONS FILE - DESIGN DOCUMENTATION
==============================================================================
FILE PURPOSE: Provides comprehensive, unambiguous instructions for AI assistants
AUDIENCE: Primarily GitHub Copilot, but applicable to all AI coding assistants
DESIGN PHILOSOPHY: Maximum clarity and enforceability through multiple reinforcement techniques

REINFORCEMENT STRATEGY OVERVIEW:
1. XML SEMANTIC TAGS: Machine-parseable blocks for critical requirements
   - <CRITICAL_REQUIREMENT>, <WORKFLOW_ENFORCEMENT>, <NAMING_REQUIREMENTS>
   - <COMMIT_REQUIREMENTS>, <PROCESS_REQUIREMENTS>, <CODING_REQUIREMENTS>

2. VISUAL REINFORCEMENT: Multiple formatting techniques for human/AI recognition
   - **Bold** for critical concepts and section headers
   - ✅/❌ for positive/negative examples with immediate visual feedback
   - `Code blocks` for exact patterns, commands, and technical specifications
   - Numbered lists for sequential processes and procedures

3. STRUCTURAL REINFORCEMENT: Hierarchical organization for logical flow
   - Main sections with HTML comment documentation blocks
   - Subsections with specific focus areas and targeted instructions
   - Examples paired with counter-examples for clarity
   - Cross-references to related documentation files

4. BEHAVIORAL REINFORCEMENT: Explicit compliance mechanisms
   - "MUST", "MUST NOT", "SHOULD" imperative language
   - Specific numeric thresholds and measurable criteria
   - Process workflows with decision points and validation steps
   - Reference links to authoritative documentation sources

SECTION ORGANIZATION:
- Project Methodologies: Development workflow, branching, commits, PRs
- Coding Standards: Quality requirements, language standards, review processes
- Repository Overview: Context and supporting documentation structure

META-DOCUMENTATION: HTML comments before each section explain the instructional
design, reinforcement techniques, and compliance mechanisms for that section.
This creates a self-documenting system for understanding how AI instructions work.
==============================================================================
-->

## Critical Requirements

- AI assistants must _never_ remove HTML comments explaining the purpose and design of each section.
- HTML comments provide essential context for understanding the instructional design and MUST remain intact.

<CRIITICAL_REQUIREMENT type="MANDATORY">
  <HTML_COMMENT presence="REQUIRED">
    AUDIENCE: Primarily humans, but applicable to all AI coding assistants
    DESIGN PHILOSOPHY: Maximum clarity and comprehensiveness through clear explanations
    PURPOSE: Explain the instructional design, reinforcement techniques, and compliance mechanisms used in this section
   </HTML_COMMENT>
</CRITICAL_REQUIREMENT>

**Audience:** Primarily GitHub Copilot. This file provides Copilot-specific instructions and configuration for this repository.

**Repository Context:**
GoSpin is a Go 1.25.6 service (module `github.com/bassista/go_spin`) that manages Docker
containers and serves a dashboard plus a JSON API. The UI in `ui/` is plain HTML and
JavaScript served by the binary — there is no `package.json`, no Node toolchain and no
TypeScript in this repository.

**Copilot Guidance:**

- `AGENTS.md` is the authoritative agent guide; when this file and `AGENTS.md` disagree,

  `AGENTS.md` wins. Keep the two in sync.

- Follow the configuration and best practices outlined here when assisting in this repository.
- Reference `README.md` for usage, configuration and the API endpoints table.

<!--
==============================================================================
CODING STANDARDS SECTION
==============================================================================
PURPOSE: Establish comprehensive code quality and consistency requirements
SCOPE: Covers general principles, project-specific standards, and quality gates
REINFORCEMENT TECHNIQUES:
- XML <CODING_REQUIREMENTS> for mandatory compliance
- Hierarchical organization from general to specific
- Cross-references to existing documentation files
- Language-specific categorization for targeted guidance
- Quality assurance checklist for verification
COMPLIANCE MECHANISM: References existing instruction files for detailed rules
==============================================================================
-->

## Coding Standards

<CODING_REQUIREMENTS type="MANDATORY">
AI assistants MUST follow these coding standards and reference project-specific guidelines when available.
</CODING_REQUIREMENTS>

**General Principles:**

- **Consistency**: Follow established patterns within the codebase
- **Readability**: Write code that is self-documenting and easy to understand
- **Maintainability**: Structure code for long-term maintenance and evolution
- **Security**: Apply secure coding practices appropriate for the technology stack
- **Performance**: Consider performance implications but prioritize readability unless performance is critical

**Project-Specific Standards:**

- **Backend Development**: Follow guidelines in `.github/instructions/backend.instructions.md`
- **Go Code**: Follow guidelines in `.github/instructions/go.instructions.md` and the

  conventions in `AGENTS.md` (idiomatic Go, `gofmt`, English comments and log messages)

- **Frontend Development**: Follow guidelines in `.github/instructions/frontend.instructions.md`

  for the plain JavaScript and HTML under `ui/`; its TypeScript/React rules do not apply to
  this repository

- **Documentation**: Follow guidelines in `.github/instructions/docs.instructions.md`

**Go-Specific Standards:**

- ✅ Packages under `internal/`, entrypoint in `cmd/server/`; keep handlers in

  `internal/api/controller/` and route registration in `internal/api/route/`

- ✅ Propagate `context.Context` through request and scheduler paths
- ✅ Wrap errors with context: `fmt.Errorf("loading actions config: %w", err)`
- ✅ Log through `internal/logger` (`logger.WithComponent("...")`), levels Debug/Info/Warn/Error
- ✅ Read every tunable value from `internal/config` (viper, `GO_SPIN_` env prefix) — no magic numbers
- ❌ Do not add `fmt.Println`, a second logger, or hardcoded ports/timeouts/intervals
- ❌ Do not add `nolint` directives to silence a genuine finding; fix the cause

**Code Review Guidelines:**

- Reference: `docs/engineering/code-review-guidelines.md`
- All code MUST pass review before merging
- Focus on correctness, security, maintainability, and adherence to standards

**Pull Request Guidelines:**

- Include clear description of changes and rationale
- Ensure PR size remains manageable (target ≤ 400 lines)
- Include relevant tests and documentation updates
- Commit subjects follow Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`,

  `refactor:`), matching recent history

**Language-Specific Standards:**

- **Go**: This is the only implementation language of the project — Go 1.25.6, idiomatic

  conventions, standard library first

- **JavaScript/HTML (frontend)**: Plain ES modules in `ui/`, no framework, no bundler, no

  transpilation step; Alpine.js, htmx and Tailwind are loaded from CDNs

- **Documentation**: Clear, concise language with proper Markdown formatting; English

  everywhere except `docs/progetto.txt` (Italian)

**Quality Assurance:**

- All code MUST include appropriate unit tests
- Integration-style tests for HTTP routes and scheduling/runtime workflows
- Documentation updates for public APIs, configuration options or runtime behaviour
- Security considerations documented and reviewed

---

<!--
==============================================================================
QUALITY & COVERAGE POLICY SECTION
==============================================================================
PURPOSE: Define a single source of truth (SSOT) for test coverage and quality
targets across the repository. Eliminates conflicting mandates in chat modes.
REINFORCEMENT TECHNIQUES:
- Stable HTML anchor for cross-file references
- Tiered numeric targets with clear enforcement and exception process
- Directive language (MUST/SHOULD) to enable automation
==============================================================================
-->

<a name="quality-policy"></a>

## Quality & Coverage Policy

Principles:

- Per-Package Floor: Every package must stay at or above 70% coverage.
- Quality > Percentage: Prefer meaningful assertions and coverage of error/security paths over chasing a numeric score.
- Transparency: Exceptions must be explicit and justified in the PR.

Targets:

- Floor for every package under `internal/` and `cmd/`: **≥ 70%**
- New or changed code: cover the new branches, including error and edge cases
- Hot paths (request handling, scheduler loops, persistence) and security-relevant logic

  (input validation, token handling): 100% of the new branches

Enforcement:

- Agent workflow: `go test ./...` plus `task test:cover` and a per-package coverage check

  before finishing any change; add tests when a package drops below the floor

- CI: `.github/workflows/tests.yml` runs `go vet`, `staticcheck`, `golangci-lint` and

  `go test -race ./...` on pushes and pull requests to `develop` and `main`;
  `.github/workflows/coverage.yml` measures the same refs but publishes the coverage report
  and badge on GitHub Pages only from `main`, and does not gate the build on a threshold

Exceptions:

- Use a PR footer section titled "Coverage Exception:" explaining scope, rationale, and risk mitigation
- Obtain at least one reviewer acknowledgment of the exception in review comments

References:

- Agents (Developer/Tester) MUST reference this section instead of hardcoding numeric targets.
- `AGENTS.md` carries the same 70% per-package threshold; keep both files aligned.

---

<!--
==============================================================================
REPOSITORY OVERVIEW SECTION
==============================================================================
PURPOSE: Provide contextual information about repository structure and resources
SCOPE: Directory organization, file purposes, and cross-reference guidance
REINFORCEMENT TECHNIQUES:
- Bullet-point directory structure for easy scanning
- Inline file examples for concrete understanding
- Cross-reference links to authoritative documentation
- Clear categorization of different resource types
DESIGN RATIONALE: Helps AI assistants understand available resources and when to use them
NOTE: This section may be removed when used as template, hence the disclaimer
==============================================================================
-->

## Repository Overview (This section may be removed if this is used as a template)

**Project Structure:**

- `cmd/server/`: entrypoint (`main.go`)
- `cmd/board/`: `gb-cli` CLI entrypoint (client of the plain-text board API)
- `internal/`: `api/{route,controller,middleware}`, `app`, `boardclient`, `cache`, `config`,
  `cosmos`, `ghostboard`, `logger`, `repository`, `runtime`, `scheduler`, `version`

- `config/`: runtime YAML (`config.yaml`, `homepage.yaml`, `actions.yml`, `*.example`) and

  `config/data/` JSON storage

- `ui/`: embedded static UI (`index.html`, `home.html`, `ghostboard.html`, `home.js`, `templates/`, `assets/`, `service-worker.js`, `sw-register.js`)
- `docs/`: `ARCHITECTURE.md`, `DEVELOPMENT.md`, ADRs, PRDs, engineering guidelines,

  `progetto.txt` (Italian)

- `Makefile` and `Taskfile.yml`: the two supported build front-ends (keep both working)

**.github Directory Structure & Purpose:**

The `.github` directory contains several subdirectories and files that organize configuration, prompts, and instructions for Copilot and other AI agents:

- `agents/`: Contains agent configuration files (`Developer.agent.md`, `Tester.agent.md`,

  `CodeReviewer.agent.md`) that define custom conversational behaviors for Copilot and other
  agents. As of October 2025, GitHub renamed "Chat Modes" to "Agents".

- `instructions/`: Holds instruction files for backend, frontend, Go and documentation. These

  guide Copilot and other agents on best practices and project-specific rules. You must apply
  these instructions to relevant files in the repository.
  - `backend.instructions.md`
  - `docs.instructions.md`
  - `frontend.instructions.md`
  - `go.instructions.md`

- `prompts/`: Includes prompt templates (`write-docs.prompt.md`, `write-adr.prompt.md` when

  present, `testcov.prompt.md`, `copilot-setup-check.prompt.md`) used to generate
  documentation, tests and setup checks.

- `workflows/`: GitHub Actions workflows: `tests.yml` (vet, staticcheck, golangci-lint,

  `go test -race`), `coverage.yml` (coverage report on GitHub Pages),
  `docker-release.yml` (sole publisher for `v*.*.*` tags, ghcr.io and Docker Hub) and
  `docker_build_push.yml` (`main` pushes, pull requests and manual branch builds).

## Developer Workflows

### Build & Run

```bash
# Quality gate first
go fmt ./...
go vet ./...
staticcheck ./...
golangci-lint run --concurrency=1 ./...
go mod tidy
go test ./...

# Build and run (Makefile → ./.build/main, injects version metadata via -ldflags)
make build
make run            # go run with version metadata
make watch          # air hot reload (.air.toml)

# Same workflows through the Taskfile (→ .build/gospin)
task build          # runs lint + test first
task build:fast
task run
task watch
```

### Test & Coverage

```bash
go test ./...
task test:race                                   # go test -race ./...
task test:cover                                  # coverage.out + coverage.html
task test:package -- ./internal/config           # single package
```

On a Raspberry Pi 4 the race detector cannot be built; skip `-race` there and say so in the summary.

### Docker & Release

```bash
task docker:build                  # local image (bassista/gospin:latest)
task docker:buildx                 # multi-arch build + push
task docker:run                    # ports 8084/8085, mounts the Docker socket
task version:show                  # prints .version
task version:bump -- 0.0.12        # bump .version, commit and tag
```

Always execute `go vet ./...` before building, testing, or committing code. Ensure code passes `go fmt ./...` for consistent formatting. Ensure code passes `golangci-lint run` for linting. Ensure all dependencies are properly managed with `go mod tidy`. Ensure all unit tests pass with `go test ./...` before pushing changes. Ensure commit messages follow conventional commit standards. Ensure code is documented with comments where necessary for clarity. Ensure security vulnerabilities are regularly scanned and addressed. Ensure dependencies are kept up to date with regular reviews. Ensure coding standards and best practices are followed throughout the codebase. Ensure proper error handling is implemented consistently. Ensure logging is used effectively for debugging and monitoring. Ensure performance considerations are taken into account during development. Ensure scalability is considered in the architecture and design decisions. Ensure user input is validated and sanitized to prevent security issues. Ensure sensitive information is not hardcoded and is managed securely. Ensure configuration management follows best practices for different environments. Ensure documentation is kept up to date with code changes.

Refer to the main README.md for a full overview of repository goals and usage. `AGENTS.md`
is the authoritative agent guide.

<!-- © Capgemini 2025 -->
