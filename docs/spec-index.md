# Tabula Spec Index

Canonical documentation for `v0.0.1`.

## Product and Behavior Specs

- Interaction model: `object-scoped-intent-ui.md`
- Review workflow and commit model: `review-mode-workflow.md`
- Public interface references: `interfaces.md`
- Architecture: `architecture.md`
- Version freeze and release notes: `release-v0.0.1.md`

## Source Code Anchors

### CLI and Runtime Entrypoints

- `cmd/tabula/main.go`
- `internal/serve/app.go`
- `internal/web/server.go`
- `internal/ptyd/app.go`

### MCP Surface

- `internal/mcp/server.go`
- `internal/canvas/adapter.go`
- `internal/canvas/events.go`

### Browser UI

- `internal/web/static/index.html`
- `internal/web/static/app.js`
- `internal/web/static/canvas.js`
- `internal/web/static/terminal.js`
- `internal/web/static/style.css`

## Scope Boundaries

- Tabula is the canvas consumer and interaction runtime.
- Producer-side source access (mail/files/calendar/etc.) is external.
- Handoff transport contracts are defined in `handoff-protocol`.
