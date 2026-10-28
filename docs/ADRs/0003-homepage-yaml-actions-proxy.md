---
title: "ADR-0003: YAML-driven homepage with a server-side actions proxy"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "homepage", "security", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0003: YAML-driven homepage with a server-side actions proxy

## Status

**Accepted**

## Context

GoSpin also serves a personal dashboard (`/ui/home`) with service cards, bookmarks, a search palette
and buttons that trigger HTTP calls on other systems (restart a service, wake a machine, run a hook).
Those calls usually need a token or an internal endpoint the browser must not know. The dashboard
content is operator-specific and changes far more often than the binary, and it must be editable on
a headless server.

## Decision

The homepage is described by `config/homepage.yaml`, which the controller reloads on every
`GET /homepage` call (`config.LoadHomepageConfig`) and returns together with a hash of the file. The
page polls the endpoint and re-renders only when the hash changes.

Actions are split in two files:

- `config/homepage.yaml` holds the visual definition (name, icon, description) and a reference key;
- `config/actions.yml` holds the technical definition (method, URL, headers, body) and never reaches
  the browser.

`POST /homepage/action` receives only the action name, looks up the definition and performs the HTTP
request server-side (`internal/api/controller/actions_controller.go`) with
`data.actions_timeout_seconds`. `data.actions_mode` decides the shape of the answer: `async`
(default) returns the upstream status, `sync` also returns the body, capped at 1 MB. A missing
`actions.yml` is not an error, and a dangling reference is reported at startup instead of failing on
click.

## Consequences

### Positive

- **POS-001**: Secrets and internal URLs stay on the server; the browser sends a name and receives a
  status, so the page holds no credential.
- **POS-002**: Editing `homepage.yaml` publishes new cards, bookmarks, themes and actions without a
  rebuild or a restart, which suits a self-hosted dashboard.
- **POS-003**: The visual and the technical halves can be changed by different people, and an action
  referenced but not defined degrades to a warning rather than a broken page.

### Negative

- **NEG-001**: `actions.yml` is privileged configuration: anyone who can write it can make the server
  call arbitrary URLs, so file permissions and the container boundary are the real security control.
- **NEG-002**: The homepage file is parsed on every request; the cost stays small because the file is
  local and small, but it is a per-request read.
- **NEG-003**: `async` and `sync` differ only in what they return, which can be surprising; `mode` is
  therefore always part of the response.

## Alternatives Considered

### Client-side `fetch` from the browser

- **ALT-001**: **Description**: Put the URL and headers directly in `homepage.yaml` and let the page
  call the target.
- **ALT-002**: **Rejection Reason**: Every token would be readable in the page source, and internal
  targets would need CORS opened to the browser.

### Actions stored in the JSON data file and edited in the UI

- **ALT-003**: **Description**: Manage actions from the dashboard, like containers and schedules.
- **ALT-004**: **Rejection Reason**: Headers and bodies are secrets; keeping them in the data file
  that the UI reads and writes would expose them through the API, and it would need a form that edits
  arbitrary HTTP requests.

### Generic plugin or script execution

- **ALT-005**: **Description**: Let an action run a local command instead of an HTTP request.
- **ALT-006**: **Rejection Reason**: GoSpin already mounts the Docker socket; adding command execution
  to a web-triggered path turns a dashboard button into a remote shell and a much larger blast radius.

## Implementation Notes

- **IMP-001**: The parsed `actions.yml` is cached behind a `sync.RWMutex` and re-read on the first
  request after a failed startup load, so a syntax error fixed at runtime recovers without a restart.
- **IMP-002**: `GET /homepage` embeds the build metadata as top-level fields; a `version` key written
  in the YAML is ignored, so what the binary was built with always wins.
- **IMP-003**: `LogDanglingReferences` warns once at startup, naming both the missing action and the
  file it looked in.

## References

- **REF-001**: [README.md - Homepage Configuration](../../README.md#homepage-configuration)
- **REF-002**: [ARCHITECTURE.md - Homepage and Actions](../ARCHITECTURE.md#homepage-and-actions)
- **REF-003**: `internal/api/controller/actions_controller.go`
