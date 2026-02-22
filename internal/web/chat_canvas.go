package web

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type canvasAction struct {
	Title   string
	Kind    string
	Content string
}

var canvasShowRe = regexp.MustCompile(`(?s):::canvas_show\{([^}]*)\}\n?(.*?):::`)

func parseCanvasActions(text string) ([]canvasAction, string) {
	matches := canvasShowRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}
	var actions []canvasAction
	cleaned := text
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		fullStart, fullEnd := m[0], m[1]
		attrsStart, attrsEnd := m[2], m[3]
		contentStart, contentEnd := m[4], m[5]

		attrs := text[attrsStart:attrsEnd]
		content := strings.TrimSpace(text[contentStart:contentEnd])

		title := extractAttr(attrs, "title")
		kind := extractAttr(attrs, "kind")
		if kind == "" {
			kind = "text"
		}

		actions = append([]canvasAction{{
			Title:   title,
			Kind:    kind,
			Content: content,
		}}, actions...)

		ref := fmt.Sprintf("[canvas: %s]", title)
		cleaned = cleaned[:fullStart] + ref + cleaned[fullEnd:]
	}
	return actions, strings.TrimSpace(cleaned)
}

var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func extractAttr(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

func (a *App) executeCanvasActions(canvasSessionID string, actions []canvasAction) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, action := range actions {
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            action.Title,
			"markdown_or_text": action.Content,
		})
	}
}

// resolveArtifactFilePath maps an artifact title to an absolute file path.
// Returns "" if title has no file-like indicator (dot or separator) or the
// resolved file does not exist on disk.
func resolveArtifactFilePath(cwd, title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	if !strings.Contains(t, ".") && !strings.Contains(t, "/") {
		return ""
	}
	var abs string
	if filepath.IsAbs(t) {
		abs = t
	} else {
		abs = filepath.Join(cwd, t)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return abs
}

// refreshCanvasFromDisk checks whether the active canvas artifact corresponds
// to a file on disk and pushes updated content via MCP if the file has changed.
func (a *App) refreshCanvasFromDisk(projectKey string) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return
	}
	sid := a.canvasSessionIDForProject(project)
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		return
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		return
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind != "text_artifact" && kind != "text" {
		return
	}
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	if title == "" || title == "<nil>" {
		return
	}
	cwd := a.cwdForProjectKey(key)
	filePath := resolveArtifactFilePath(cwd, title)
	if filePath == "" {
		return
	}
	diskBytes, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	diskContent := string(diskBytes)
	currentText, _ := active["text"].(string)
	if strings.TrimSpace(diskContent) == strings.TrimSpace(currentText) {
		return
	}
	_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       sid,
		"kind":             "text",
		"title":            title,
		"markdown_or_text": diskContent,
	})
}
