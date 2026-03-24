package web

import (
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type localDirectCanvasTextAction struct {
	Title string
	Body  string
	Reply string
}

func parseLocalDirectCanvasTextAction(text string) (localDirectCanvasTextAction, bool) {
	clean := strings.TrimSpace(text)
	lower := strings.ToLower(clean)
	if clean == "" || !strings.Contains(lower, "canvas") {
		return localDirectCanvasTextAction{}, false
	}
	titleMarker := strings.Index(lower, " titled ")
	if titleMarker < 0 {
		return localDirectCanvasTextAction{}, false
	}
	bodyMarker := -1
	bodyPrefix := ""
	for _, marker := range []string{" with the exact body ", " with body ", " body "} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			bodyMarker = idx
			bodyPrefix = marker
			break
		}
	}
	if bodyMarker <= titleMarker {
		return localDirectCanvasTextAction{}, false
	}
	title := normalizeLocalShortcutText(clean[titleMarker+len(" titled ") : bodyMarker])
	bodyTail := clean[bodyMarker+len(bodyPrefix):]
	if dot := strings.Index(bodyTail, "."); dot >= 0 {
		bodyTail = bodyTail[:dot]
	}
	body := normalizeLocalShortcutText(bodyTail)
	if title == "" || body == "" {
		return localDirectCanvasTextAction{}, false
	}
	return localDirectCanvasTextAction{
		Title: title,
		Body:  body,
		Reply: parseLocalDirectReplyWord(clean),
	}, true
}

func parseLocalDirectReplyWord(text string) string {
	lower := strings.ToLower(text)
	marker := "reply with the single word "
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	rest := normalizeLocalShortcutText(text[idx+len(marker):])
	if rest == "" {
		return ""
	}
	for _, sep := range []string{".", "\n", " "} {
		if cut := strings.Index(rest, sep); cut >= 0 {
			rest = rest[:cut]
			break
		}
	}
	return normalizeLocalShortcutText(rest)
}

func normalizeLocalShortcutText(text string) string {
	return strings.Trim(strings.TrimSpace(text), `"'`)
}

func (a *App) tryRunDirectLocalCanvasTextTurn(sessionID string, session store.ChatSession, userText string) (string, []map[string]interface{}, bool) {
	if a == nil {
		return "", nil, false
	}
	action, ok := parseLocalDirectCanvasTextAction(userText)
	if !ok {
		return "", nil, false
	}
	workspace, err := a.effectiveWorkspaceForChatSession(session)
	if err != nil {
		return "", nil, false
	}
	mcpURL := strings.TrimSpace(workspace.MCPURL)
	if mcpURL == "" {
		mcpURL = strings.TrimSpace(a.localMCPURL)
	}
	if mcpURL == "" {
		return "", nil, false
	}
	arguments := map[string]interface{}{
		"session_id":       a.canvasSessionIDForWorkspace(workspace),
		"kind":             "text",
		"title":            action.Title,
		"markdown_or_text": action.Body,
	}
	result, err := mcpToolsCallURL(mcpURL, "canvas_artifact_show", arguments)
	if err != nil {
		return "", nil, false
	}
	reply := strings.TrimSpace(action.Reply)
	if reply == "" {
		reply = "Done."
	}
	return reply, []map[string]interface{}{{
		"type":         "mcp_tool",
		"name":         "canvas_artifact_show",
		"arguments":    arguments,
		"result":       result,
		"is_error":     false,
		"workspace_id": workspace.ID,
	}}, true
}
