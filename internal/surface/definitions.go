package surface

import "strings"

const (
	ProtocolBlockBeginMarker = "<!-- TABURA_PROTOCOL:BEGIN -->"
	ProtocolBlockEndMarker   = "<!-- TABURA_PROTOCOL:END -->"
)

type ToolProperty struct {
	Type        string
	Description string
	Enum        []string
}

type Tool struct {
	Name        string
	Description string
	Required    []string
	Properties  map[string]ToolProperty
}

type RouteSection struct {
	Title  string
	Routes []string
}

var MCPTools = []Tool{
	{
		Name:        "canvas_session_open",
		Description: "Open canvas session and initialize runtime status.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_artifact_show",
		Description: "Show one artifact kind in canvas: text, image, pdf, or clear.",
		Required:    []string{"session_id", "kind"},
	},
	{
		Name:        "canvas_status",
		Description: "Get current session status and active artifact metadata.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_import_handoff",
		Description: "Consume a generic producer handoff and render it in canvas.",
		Required:    []string{"session_id", "handoff_id"},
	},
	{
		Name: "delegate_to_model",
		Description: "Delegate a task to another model via the Codex app-server. " +
			"Use this when the task benefits from a different model's strengths: " +
			"complex multi-file coding (codex), deep analysis or architecture (codex), " +
			"or general-purpose reasoning (gpt). " +
			"Also use when the user explicitly asks to route to a model " +
			"(e.g. 'let codex handle this', 'ask gpt'). " +
			"Always provide context (conversation summary) and system_prompt (task-specific instructions) " +
			"so the delegate model has enough information. " +
			"Do NOT delegate simple conversational replies or short factual answers.",
		Required: []string{"prompt"},
		Properties: map[string]ToolProperty{
			"prompt": {
				Type:        "string",
				Description: "The task or question for the delegate model.",
			},
			"model": {
				Type:        "string",
				Description: "Model to use. Aliases: 'spark' (gpt-5.3-codex-spark), 'codex' (gpt-5.3-codex), 'gpt' (gpt-5.2). Defaults to 'codex'.",
				Enum:        []string{"spark", "codex", "gpt"},
			},
			"context": {
				Type:        "string",
				Description: "Summary of the conversation so far, giving the delegate model background.",
			},
			"system_prompt": {
				Type:        "string",
				Description: "Task-specific instructions for the delegate model (e.g. 'You are a code reviewer. Be thorough.').",
			},
			"cwd": {
				Type:        "string",
				Description: "Working directory for the delegate model. Defaults to the active project directory.",
			},
			"timeout_seconds": {
				Type:        "integer",
				Description: "Maximum time in seconds for the delegate to complete. Defaults to 600 (10 minutes).",
			},
		},
	},
}

var MCPDaemonRoutes = []string{
	"POST /mcp",
	"GET /mcp",
	"DELETE /mcp",
	"GET /ws/canvas",
	"GET /files/*",
	"GET /health",
}

var WebRouteSections = []RouteSection{
	{
		Title: "Auth and setup",
		Routes: []string{
			"GET /api/setup",
			"POST /api/setup",
			"POST /api/login",
			"POST /api/logout",
		},
	},
	{
		Title: "Runtime and chat session management",
		Routes: []string{
			"GET /api/runtime",
			"GET /api/projects",
			"POST /api/projects",
			"POST /api/projects/{project_id}/activate",
			"GET /api/projects/{project_id}/context",
			"POST /api/chat/sessions",
			"GET /api/chat/sessions/{session_id}/history",
			"GET /api/chat/sessions/{session_id}/activity",
			"POST /api/chat/sessions/{session_id}/messages",
			"POST /api/chat/sessions/{session_id}/commands",
			"POST /api/chat/sessions/{session_id}/cancel",
		},
	},
	{
		Title: "Canvas/files",
		Routes: []string{
			"GET /api/canvas/{session_id}/snapshot",
			"GET /api/files/{session_id}/*",
		},
	},
	{
		Title: "Mail interaction endpoints",
		Routes: []string{
			"POST /api/mail/action-capabilities",
			"POST /api/mail/read",
			"POST /api/mail/mark-read",
			"POST /api/mail/action",
			"POST /api/mail/draft-reply",
			"POST /api/mail/draft-intent",
			"POST /api/mail/stt",
		},
	},
	{
		Title: "Websocket routes",
		Routes: []string{
			"GET /ws/chat/{session_id}",
			"GET /ws/canvas/{session_id}",
		},
	},
}

func MCPToolNamesCSV() string {
	names := make([]string, 0, len(MCPTools))
	for _, tool := range MCPTools {
		names = append(names, "`"+tool.Name+"`")
	}
	return strings.Join(names, ", ")
}
