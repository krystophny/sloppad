# tabula

Tabula is a local-first MCP canvas and review runtime.

It provides:
- MCP server tools for canvas sessions and marks
- Browser canvas UI with review workflows
- Web runtime with auth, terminal, and canvas relays
- Handoff import path for producer/consumer integrations

License: MIT (`LICENSE`)

## Start Here

- Spec index: `docs/spec-index.md`
- Interaction model: `docs/object-scoped-intent-ui.md`
- Review workflow: `docs/review-mode-workflow.md`
- Interfaces: `docs/interfaces.md`
- Architecture: `docs/architecture.md`
- Release freeze notes: `docs/release-v0.0.1.md`

## Install

```bash
go build ./cmd/tabula
go install ./cmd/tabula
```

Requirements:
- Go 1.24+

## Core Commands

```bash
tabula bootstrap --project-dir .
tabula mcp-server --project-dir . --headless --no-canvas
tabula serve --project-dir . --host 127.0.0.1 --port 9420
tabula web --data-dir ~/.tabula-web --project-dir . --host 127.0.0.1 --port 8420
tabula ptyd --data-dir ~/.local/share/tabula-ptyd --host 127.0.0.1 --port 9333
tabula canvas
```

## Local Integration Defaults

- Web UI: `http://localhost:8420`
- MCP HTTP: `http://127.0.0.1:9420/mcp`
- Canvas websocket: `ws://127.0.0.1:9420/ws/canvas`
- Local browser session id: `local`

## Quick Handoff Import Example

```bash
PRODUCER=http://127.0.0.1:8090/mcp
CONSUMER=http://127.0.0.1:9420/mcp

handoff_id=$(
  curl -sS -X POST "$PRODUCER" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"mail_headers","selector":{"provider":"work","folder":"INBOX","limit":20}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"$PRODUCER\",\"title\":\"Inbox (20)\"}}}"
```

## Tests

```bash
go test ./...
npm run test:reports
```

Test report artifacts are written under `.tabula/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
