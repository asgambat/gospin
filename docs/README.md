# GoSpin Documentation Index

Welcome to the documentation for **GoSpin**, a Go application for scheduled management of Docker containers.

## Documentation Overview

| Document | Description | Target audience |
|----------|-------------|-----------------|
| [README.md](../README.md) | Main project documentation: quick start, configuration, homepage, API reference, deployment | All users |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Development environment setup, testing strategy, build and release workflow | Developers, contributors |
| [ARCHITECTURE.md](ARCHITECTURE.md) | System architecture, design patterns, component deep dive and data flow | Technical architects, senior developers |
| [FRONTEND.md](FRONTEND.md) | UI rules: files and routes, Alpine.js patterns, PWA and service worker, UI testing | Frontend contributors |
| [CLI.md](CLI.md) | `gb-cli` reference: commands, output modes, exit codes, configuration resolution | Script and automation users |
| [EXAMPLES.md](EXAMPLES.md) | Configuration, monitoring and troubleshooting recipes | All users, solution architects |
| [AGENTS.md](../AGENTS.md) | Agent rules: build commands, testing and coverage policy, documentation policy | AI agents, contributors |
| [engineering/code-review-guidelines.md](engineering/code-review-guidelines.md) | Review checklist, severity taxonomy and coverage-exception process | Reviewers, contributors |
| [ADRs/](ADRs) | Architecture decision records and their template, indexed in [Images and Decisions](#images-and-decisions) | Architects, maintainers |
| [screenshots/](screenshots) | Captured UI screens, from a demo instance with fictional data | All users, documentation contributors |
| [PRDs/prd-template.md](PRDs/prd-template.md) | Template for product requirement documents | Maintainers |
| [plans/plan-template.md](plans/plan-template.md) | Template for implementation plans and design documents | Maintainers |
| [plans/DOCS_REMAINING_WORK.md](plans/DOCS_REMAINING_WORK.md) | Open documentation items that need a maintainer decision | Maintainers |
| [plans/board-uploads-spec.md](plans/board-uploads-spec.md) | Specification of the ghostboard file attachments: configuration, API, lifecycle, UI and CLI | Maintainers |
| [.github/branching-strategy.md](../.github/branching-strategy.md) | Branching model, commit conventions, PR size and review rules | Contributors |
| [.github/pull_request_template.md](../.github/pull_request_template.md) | Pull request checklist | Contributors |
| [gospin.postman_collection.json](gospin.postman_collection.json) | Postman collection for the HTTP API | Integrators |
| [../CHANGELOG.md](../CHANGELOG.md) | Notable changes per release | All users, maintainers |

The only Italian document is the archived plan in [plans/archive/HTMX_MIGRATION_PLAN.md](plans/archive/HTMX_MIGRATION_PLAN.md); every other page is in English.

## Quick Navigation by Role

### New users

1. Start here: [README.md - Quick Start](../README.md#quick-start)
2. Learn by example: [EXAMPLES.md](EXAMPLES.md)
3. Try the API: [README.md - API Endpoints](../README.md#api-endpoints)
4. Use the web UI: [README.md - Web UI](../README.md#web-ui)
5. Configure the homepage: [README.md - Homepage Configuration](../README.md#homepage-configuration)

### Developers

1. Set up the environment: [DEVELOPMENT.md - Setup Development Environment](DEVELOPMENT.md#setup-development-environment)
2. Understand the architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
3. Learn the patterns: [ARCHITECTURE.md - Core Patterns](ARCHITECTURE.md#core-patterns)
4. Work on the UI: [FRONTEND.md](FRONTEND.md)
5. Write tests: [DEVELOPMENT.md - Testing Guidelines](DEVELOPMENT.md#testing-guidelines)
6. Contribute: [DEVELOPMENT.md - Contributing Guidelines](DEVELOPMENT.md#contributing-guidelines)
7. Agent rules: [AGENTS.md](../AGENTS.md)

### DevOps engineers

1. Production deployment: [README.md - Docker Deployment](../README.md#docker-deployment)
2. Security configuration: [README.md - Security](../README.md#security)
3. Monitoring setup: [EXAMPLES.md - Monitoring and Automation Examples](EXAMPLES.md#monitoring-and-automation-examples)
4. Troubleshooting: [README.md - Troubleshooting](../README.md#troubleshooting)
5. Release process: [README.md - Releasing a New Version](../README.md#releasing-a-new-version)

### Architects

1. System overview: [ARCHITECTURE.md - System Architecture Overview](ARCHITECTURE.md#system-architecture-overview)
2. Design decisions: [ARCHITECTURE.md - Design Principles](ARCHITECTURE.md#design-principles) and
   [Key Design Decisions](ARCHITECTURE.md#key-design-decisions)
3. Concurrency, shutdown and measured performance:
   [Concurrency Model](ARCHITECTURE.md#concurrency-model) and
   [Performance and Resources](ARCHITECTURE.md#performance-and-resources)
4. Extension points: [ARCHITECTURE.md - Extension Points](ARCHITECTURE.md#extension-points)
5. Integration patterns: [EXAMPLES.md](EXAMPLES.md)
6. Reference material: [screenshots and decision records](#images-and-decisions)

## Document Details

### Main Documentation (README.md)

- Content: project overview, installation, configuration, homepage and actions, API reference, deployment
- Maintenance: updated with each release

### Development Guide (DEVELOPMENT.md)

- Content: setup instructions, testing strategy, build, release and contribution workflow
- Target: contributors and the development team

### Architecture Guide (ARCHITECTURE.md)

- Content: system design, patterns, component interactions, homepage and Cosmos integration
- Target: technical architects and senior developers

### Frontend Guide (FRONTEND.md)

- Content: UI file and route map, Alpine.js conventions, PWA and service worker rules
- Target: anyone changing `ui/`

### Examples & Use Cases (EXAMPLES.md)

- Content: real-world scenarios, configuration examples, scripts
- Target: all users seeking practical guidance

### Agent Guide (AGENTS.md)

- Content: build commands, testing and coverage rules, documentation policy
- Target: AI agents and contributors

### Templates and Policies

- `ADRs/adr-template.md`, `PRDs/prd-template.md`, `plans/plan-template.md`: use the template matching the document you are writing, and keep the result in the same directory.
- `engineering/code-review-guidelines.md`: review checklist, severity taxonomy and the coverage-exception process.
- `.github/branching-strategy.md`: branching model, commit conventions and PR limits; `.github/pull_request_template.md` is the checklist every PR must fill in.

## Images and Decisions

One index for the two kinds of reference material that sit outside the guides: the captured UI and
the architecture decision records.

### Screenshots

All five images in [screenshots/](screenshots) come from a demo instance (memory runtime, fictional
containers, hosts and schedules), and the README embeds them under
[Screenshots](../README.md#screenshots).

| Image | Shows |
| ----- | ----- |
| [containers.png](screenshots/containers.png) | Containers tab: search, favourite and pinned filters, running state, CPU and memory per container |
| [groups.png](screenshots/groups.png) | Groups tab: containers per group and group-level start/stop |
| [schedules.png](screenshots/schedules.png) | Schedules tab: start and stop windows per container or group |
| [home.png](screenshots/home.png) | Homepage: services, bookmarks and proxied actions defined in `homepage.yaml` |
| [ghostboard.png](screenshots/ghostboard.png) | Ghostboard: a shared text board with live updates, files and no login |

Regenerate them with `task screenshots` (`scripts/screenshots/`): it builds the server, runs it on the
memory runtime with the fictional fixtures in `scripts/screenshots/fixtures/`, and captures the five
pages with Playwright's Chromium, installed under `.build/screenshot-deps` on first use. Node is
required only for this task. A host without an emoji font renders the UI icons as boxes, and the
script warns when it detects that.

### Architecture Decision Records

| Record | Decision |
| ------ | -------- |
| [ADR-0001](ADRs/0001-polling-scheduler.md) | Polling scheduler for start/stop windows |
| [ADR-0002](ADRs/0002-ghostboard-sse.md) | Server-Sent Events for the ghostboard |
| [ADR-0003](ADRs/0003-homepage-yaml-actions-proxy.md) | YAML-driven homepage with a server-side actions proxy |
| [ADR-0004](ADRs/0004-build-metadata-ldflags.md) | Build metadata injected with `-ldflags` from one bump script |
| [ADR-0005](ADRs/0005-gb-cli-separate-binary.md) | `gb-cli` as a separate binary with its own configuration |
| [ADR-0006](ADRs/0006-json-persistence-dirty-cache.md) | JSON data file with an in-memory cache and dirty tracking |
| [ADR-0007](ADRs/0007-taskfile-build-frontend.md) | Taskfile as the only build front-end |
| [ADR-0008](ADRs/0008-ghostboard-pin-protection.md) | PIN protection for private ghostboards |
| [ADR-0009](ADRs/0009-ghostboard-file-attachments.md) | Board-scoped file attachments, always served as downloads |

New records follow [ADRs/adr-template.md](ADRs/adr-template.md) and are added to this table.

## Search & Navigation Tips

### Finding Information

- API endpoints: search for the HTTP method (GET, POST, DELETE) in README.md
- Configuration options: look for YAML examples or environment variables
- Error resolution: check the Troubleshooting section in README.md
- Code examples: found throughout EXAMPLES.md and DEVELOPMENT.md
- Architecture patterns: detailed in ARCHITECTURE.md with diagrams
- Build, test and coverage rules: in AGENTS.md

### Cross-References

Documents are cross-referenced:

- Links to related sections in other documents
- API examples reference configuration options
- Architecture concepts link to implementation details
- Use cases demonstrate configuration patterns

## Getting Started Checklist

### For development

- [ ] Read the [README.md](../README.md) overview
- [ ] Follow the [DEVELOPMENT.md setup](DEVELOPMENT.md#setup-development-environment)
- [ ] Review the [ARCHITECTURE.md patterns](ARCHITECTURE.md#core-patterns)
- [ ] Try the [EXAMPLES.md scenarios](EXAMPLES.md)
- [ ] Run `task check` and contribute

### For production

- [ ] Understand the [README.md features](../README.md#features)
- [ ] Plan the deployment with [README.md - Production Deployment](../README.md#production-deployment)
- [ ] Configure the [security settings](../README.md#security)
- [ ] Set up monitoring and alerts
- [ ] Test with the example scenarios

### For integration

- [ ] Review the [API Endpoints](../README.md#api-endpoints)
- [ ] Import the [Postman Collection](gospin.postman_collection.json)
- [ ] Study the [gb-cli documentation](../README.md#the-gb-cli-cli) for script integration
- [ ] Test integration scenarios
- [ ] Plan automation workflows

## Support & Resources

### Community Resources

- GitHub repository: [asgambat/gospin](https://github.com/asgambat/gospin)
- Issues and bug reports: use GitHub Issues
- Feature requests: GitHub Discussions
- API testing: Postman collection included

### Additional Tools

- Postman Collection: `gospin.postman_collection.json`
- Configuration examples: `../config/config.yaml`
- Docker compositions: `../docker-compose.yml`, `../dev.docker-compose.yml`
- Hot reload configs: `../.air.toml`, `../.air_win.toml`

### Maintenance

- README.md: updated with each release
- User-facing guides: updated in the same change that alters the behaviour they describe
- Examples: extended when a new feature ships
- API reference: kept in sync with the routes in `internal/api/route/`
