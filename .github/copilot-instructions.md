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

**CRITICAL REQUIREMENTS**
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
This repository is a GitHub Template created by Capgemini's AI & Software Engineering team. It contains practical, portable, and useful examples of Copilot configuration for real-world projects.

**Copilot Guidance:**
- Follow the configuration and best practices outlined here when assisting in this repository.
- Reference `README.md` for general orientation and project goals.


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
- **Frontend Development**: Follow guidelines in `.github/instructions/frontend.instructions.md`
- **Documentation**: Follow guidelines in `.github/instructions/docs.instructions.md`
- **BDD Testing**: Follow guidelines in `.github/instructions/bdd-tests.instructions.md`

**Code Review Guidelines:**
- Reference: `docs/engineering/code-review-guidelines.md` (when available)
- All code MUST pass review before merging
- Focus on correctness, security, maintainability, and adherence to standards

**Pull Request Guidelines:**
- Reference: `docs/engineering/pull-request-guidelines.md` (when available)
- Include clear description of changes and rationale
- Ensure PR size remains manageable (target ≤ 400 lines)
- Include relevant tests and documentation updates

**Language-Specific Standards:**
- **GO**: Follow Go idiomatic conventions and best practices
- **Java/Spring Boot**: Follow Spring Boot conventions and best practices
- **Python/Django**: Follow PEP 8 and Django conventions
- **C#/.NET**: Follow Microsoft C# coding conventions
- **JavaScript/TypeScript**: Follow established project linting rules
- **Documentation**: Use clear, concise language with proper Markdown formatting

**Quality Assurance:**
- All code MUST include appropriate unit tests
- Integration tests for complex workflows
- Documentation updates for public APIs or significant changes
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
- Tiered Targets: Apply realistic thresholds by test type and importance.
- Quality > Percentage: Prefer meaningful assertions and coverage of error/security paths over chasing a numeric score.
- Transparency: Exceptions must be explicit and justified in the PR.

Tiered Targets:
- Core domain logic: target ≥ 95% line/branch coverage
- Integrations/adapters: target ≥ 85%
- Generated scaffolds and spikes: opportunistic; may be exempt if tagged and justified in PR

Enforcement:
- Global threshold: CI fails if overall repository coverage < 70%
- Module threshold: CI fails if any core module drops below its target (≥ 80%)

Critical Coverage (must be 100%):
- Hot paths (performance- or user-critical flows)
- Error and exception paths (including negative and edge-case handling)
- Security-relevant logic (authn/authz, input validation, data protection)

Exceptions:
- Use a PR footer section titled "Coverage Exception:" explaining scope, rationale, and risk mitigation
- Obtain at least one reviewer acknowledgment of the exception in review comments

References:
- Agents (Developer/Tester) MUST reference this section instead of hardcoding numeric targets.
- Project-specific overrides, if any, MUST be documented here to remain authoritative.

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

**.github Directory Structure & Purpose:**

The `.github` directory contains several subdirectories and files that organize configuration, prompts, and instructions for Copilot and other AI agents:

- `agents/`: Contains agent configuration files (e.g., `Developer.agent.md`) that define custom conversational behaviors for Copilot and other agents. As of October 2025, GitHub renamed "Chat Modes" to "Agents".

- `instructions/`: Holds instruction files for backend, frontend, and documentation. These guide Copilot and other agents on best practices and project-specific rules. You must apply these instructions to relevant files in the repository.
  - `backend.instructions.md`
  - `docs.instructions.md`
  - `frontend.instructions.md`

- `prompts/`: Includes prompt templates (e.g., `write-adr.prompt.md`, `write-docs.prompt.md`, `write-prd.prompt.md`) used to generate architectural decision records, documentation, and product requirements. These help standardize and accelerate content creation.

- `workflows/`: Intended for GitHub Actions workflow files, which automate CI/CD and other repository tasks. (Currently empty, but will be expanded.)


## Developer Workflows

### Build & Run
```bash
go vet ./...
go fmt ./...
golangci-lint run
go mod tidy
go test ./...
go build -o .build/main ./cmd/server/main.go
./.build/main
```

Always execute `go vet ./...` before building, testing, or committing code. Ensure code passes `go fmt ./...` for consistent formatting. Ensure code passes `golangci-lint run` for linting. Ensure all dependencies are properly managed with `go mod tidy`. Ensure all unit tests pass with `go test ./...` before pushing changes. Ensure commit messages follow conventional commit standards. Ensure code is documented with comments where necessary for clarity. Ensure security vulnerabilities are regularly scanned and addressed. Ensure dependencies are kept up to date with regular reviews. Ensure coding standards and best practices are followed throughout the codebase. Ensure proper error handling is implemented consistently. Ensure logging is used effectively for debugging and monitoring. Ensure performance considerations are taken into account during development. Ensure scalability is considered in the architecture and design decisions. Ensure user input is validated and sanitized to prevent security issues. Ensure sensitive information is not hardcoded and is managed securely. Ensure configuration management follows best practices for different environments. Ensure documentation is kept up to date with code changes.


Refer to the main README.md for a full overview of repository goals and usage.


<!-- © Capgemini 2025 -->
