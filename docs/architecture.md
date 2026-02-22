# Tabura Architecture

Tabura is a Go-first MCP canvas/runtime stack with a browser UI.

## Components

- `cmd/tabura/main.go`
  - CLI entrypoint and subcommand dispatch.
- `internal/mcp/server.go`
  - MCP JSON-RPC methods and tool dispatch.
- `internal/canvas/adapter.go`
  - Canvas sessions, artifact state, and event log.
- `internal/serve/app.go`
  - MCP HTTP daemon (`/mcp`) and canvas websocket (`/ws/canvas`).
- `internal/web/server.go`
  - Browser APIs for chat sessions, canvas/mail actions, and chat/canvas websocket routes.
- `internal/store/store.go`
  - SQLite persistence for auth and chat session/message history.
- `internal/protocol/bootstrap.go`
  - Bootstrap behavior for project-local integration files.

## Runtime Modes

- `tabura mcp-server`: stdio MCP runtime
- `tabura serve`: HTTP MCP + canvas websocket runtime
- `tabura web`: browser-facing runtime
- `tabura canvas`: convenience browser launcher

## UI Layout

The browser UI uses a unified canvas surface:

- **Chat** is the default pane in the canvas viewport.
- **Artifacts** (text, image, PDF) appear as closeable tabs in the canvas tab bar.
- A single **prompt bar** (`#prompt-input` + `#prompt-send`) serves all modes.
- No dual-mode switching between chat and canvas panels.

## Primary Data Flows

1. MCP client calls tool on `tabura mcp-server` or `tabura serve`.
2. Tool dispatch in `internal/mcp/server.go` resolves into adapter operations.
3. Adapter updates session/artifact state in memory and emits events.
4. Browser consumes websocket events: chat messages render in the chat pane, artifacts open as new tabs.

## Artifact Interaction (Tap-to-Reference)

Users interact with artifacts via tap-to-reference rather than persistent annotations:

- **Tap/click** on artifact text places a transient marker and captures the line location as context in the prompt bar.
- **Long-press** on artifact text places a marker and starts push-to-talk voice recording; release sends the transcription with location context.
- **Text selection** captures the selected text and line as context.
- Location context is prepended to the chat message as `[Line N of "title"]` and cleared after send.

No persistent marks, overlays, or commit lifecycle.

## Handoff Import Flow

1. Producer creates handoff payload (outside Tabura).
2. Tabura receives `canvas_import_handoff` with `handoff_id`.
3. Tabura peeks/consumes producer handoff payload and renders artifact.

## Trust and Access Boundaries

- Tabura does not require direct credentials to producer systems.
- Producer endpoint authority remains outside Tabura.
- Tabura stores local auth/session state in SQLite under web data dir.
