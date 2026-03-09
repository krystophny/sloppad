package web

import (
	"fmt"
	"strings"
	"sync"
)

type chatCursorContext struct {
	View          string  `json:"view,omitempty"`
	Element       string  `json:"element,omitempty"`
	Title         string  `json:"title,omitempty"`
	Page          int     `json:"page,omitempty"`
	Line          int     `json:"line,omitempty"`
	RelativeX     float64 `json:"relative_x,omitempty"`
	RelativeY     float64 `json:"relative_y,omitempty"`
	SelectedText  string  `json:"selected_text,omitempty"`
	Surrounding   string  `json:"surrounding_text,omitempty"`
	ItemID        int64   `json:"item_id,omitempty"`
	ItemTitle     string  `json:"item_title,omitempty"`
	ItemState     string  `json:"item_state,omitempty"`
	WorkspaceID   int64   `json:"workspace_id,omitempty"`
	WorkspaceName string  `json:"workspace_name,omitempty"`
	Path          string  `json:"path,omitempty"`
	IsDir         bool    `json:"is_dir,omitempty"`
}

type chatCursorContextTracker struct {
	mu     sync.Mutex
	queues map[string][]*chatCursorContext
}

func newChatCursorContextTracker() *chatCursorContextTracker {
	return &chatCursorContextTracker{
		queues: map[string][]*chatCursorContext{},
	}
}

func normalizeChatCursorContext(raw *chatCursorContext) *chatCursorContext {
	if raw == nil {
		return nil
	}
	ctx := &chatCursorContext{
		View:          strings.ToLower(strings.TrimSpace(raw.View)),
		Element:       strings.ToLower(strings.TrimSpace(raw.Element)),
		Title:         strings.TrimSpace(raw.Title),
		Page:          raw.Page,
		Line:          raw.Line,
		RelativeX:     raw.RelativeX,
		RelativeY:     raw.RelativeY,
		SelectedText:  strings.TrimSpace(raw.SelectedText),
		Surrounding:   strings.TrimSpace(raw.Surrounding),
		ItemID:        raw.ItemID,
		ItemTitle:     strings.TrimSpace(raw.ItemTitle),
		ItemState:     strings.ToLower(strings.TrimSpace(raw.ItemState)),
		WorkspaceID:   raw.WorkspaceID,
		WorkspaceName: strings.TrimSpace(raw.WorkspaceName),
		Path:          strings.Trim(strings.ReplaceAll(raw.Path, "\\", "/"), " /"),
		IsDir:         raw.IsDir,
	}
	if ctx.Page < 0 {
		ctx.Page = 0
	}
	if ctx.Line < 0 {
		ctx.Line = 0
	}
	if ctx.RelativeX < 0 || ctx.RelativeX > 1 {
		ctx.RelativeX = 0
	}
	if ctx.RelativeY < 0 || ctx.RelativeY > 1 {
		ctx.RelativeY = 0
	}
	if ctx.ItemID < 0 {
		ctx.ItemID = 0
	}
	if ctx.WorkspaceID < 0 {
		ctx.WorkspaceID = 0
	}
	if ctx.Title == "" &&
		ctx.Page == 0 &&
		ctx.Line == 0 &&
		ctx.RelativeX == 0 &&
		ctx.RelativeY == 0 &&
		ctx.SelectedText == "" &&
		ctx.Surrounding == "" &&
		ctx.View == "" &&
		ctx.Element == "" &&
		ctx.ItemID == 0 &&
		ctx.ItemTitle == "" &&
		ctx.ItemState == "" &&
		ctx.WorkspaceID == 0 &&
		ctx.WorkspaceName == "" &&
		ctx.Path == "" {
		return nil
	}
	return ctx
}

func (c *chatCursorContext) hasPointedItem() bool {
	return c != nil && c.ItemID > 0
}

func (c *chatCursorContext) hasPointedPath() bool {
	return c != nil && strings.TrimSpace(c.Path) != ""
}

func (t *chatCursorContextTracker) enqueue(sessionID string, raw *chatCursorContext) {
	if t == nil {
		return
	}
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queues[cleanSessionID] = append(t.queues[cleanSessionID], normalizeChatCursorContext(raw))
}

func (t *chatCursorContextTracker) consume(sessionID string) *chatCursorContext {
	if t == nil {
		return nil
	}
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	queue := t.queues[cleanSessionID]
	if len(queue) == 0 {
		return nil
	}
	next := queue[0]
	if len(queue) == 1 {
		delete(t.queues, cleanSessionID)
	} else {
		t.queues[cleanSessionID] = queue[1:]
	}
	return next
}

func appendChatCursorPrompt(prompt string, cursor *chatCursorContext) string {
	contextBlock := formatChatCursorPromptContext(cursor)
	prompt = strings.TrimSpace(prompt)
	if contextBlock == "" {
		return prompt
	}
	if prompt == "" {
		return contextBlock
	}
	return contextBlock + "\n\n" + prompt
}

func formatChatCursorPromptContext(cursor *chatCursorContext) string {
	cursor = normalizeChatCursorContext(cursor)
	if cursor == nil {
		return ""
	}

	if cursor.hasPointedItem() {
		target := fmt.Sprintf("item #%d", cursor.ItemID)
		if title := firstNonEmptyCursorText(cursor.ItemTitle, cursor.Title); title != "" {
			target += fmt.Sprintf(" %q", title)
		}
		details := make([]string, 0, 2)
		if cursor.ItemState != "" {
			details = append(details, "state: "+cursor.ItemState)
		}
		if cursor.WorkspaceName != "" {
			details = append(details, "workspace: "+cursor.WorkspaceName)
		}
		lines := []string{"## Cursor Context"}
		if cursor.View != "" {
			lines = append(lines, "User is in: "+cursor.View+" view")
		}
		lines = append(lines, "User is pointing at: "+appendCursorDetails(target, details))
		return strings.Join(lines, "\n")
	}

	if cursor.hasPointedPath() {
		targetKind := "file"
		if cursor.IsDir {
			targetKind = "folder"
		}
		target := fmt.Sprintf("%s %q", targetKind, cursor.Path)
		details := make([]string, 0, 1)
		if cursor.WorkspaceName != "" {
			details = append(details, "workspace: "+cursor.WorkspaceName)
		}
		lines := []string{"## Cursor Context"}
		if cursor.View != "" {
			lines = append(lines, "User is in: "+cursor.View+" view")
		}
		lines = append(lines, "User is pointing at: "+appendCursorDetails(target, details))
		return strings.Join(lines, "\n")
	}

	targetParts := make([]string, 0, 4)
	if cursor.Page > 0 {
		targetParts = append(targetParts, fmt.Sprintf("page %d", cursor.Page))
	}
	if cursor.Line > 0 {
		targetParts = append(targetParts, fmt.Sprintf("line %d", cursor.Line))
	}
	if cursor.RelativeX > 0 || cursor.RelativeY > 0 {
		targetParts = append(targetParts, fmt.Sprintf("point %.0f%%, %.0f%%", cursor.RelativeX*100, cursor.RelativeY*100))
	}
	target := strings.Join(targetParts, ", ")
	if title := strings.TrimSpace(cursor.Title); title != "" {
		if target != "" {
			target += " of "
		}
		target += fmt.Sprintf("%q", title)
	}
	if target == "" {
		target = "active artifact"
	}
	lines := []string{
		"## Cursor Context",
	}
	if cursor.View != "" {
		lines = append(lines, "User is in: "+cursor.View+" view")
	}
	lines = append(lines, "User is pointing at: "+target)
	if text := strings.TrimSpace(cursor.SelectedText); text != "" {
		lines = append(lines, "Selected text: "+quotePromptText(text, 220))
	}
	if text := strings.TrimSpace(cursor.Surrounding); text != "" {
		lines = append(lines, "Surrounding text:")
		lines = append(lines, limitPromptLines(text, 6, 420))
	}
	return strings.Join(lines, "\n")
}

func appendCursorDetails(target string, details []string) string {
	filtered := make([]string, 0, len(details))
	for _, detail := range details {
		if clean := strings.TrimSpace(detail); clean != "" {
			filtered = append(filtered, clean)
		}
	}
	if len(filtered) == 0 {
		return target
	}
	return target + " (" + strings.Join(filtered, ", ") + ")"
}

func firstNonEmptyCursorText(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func quotePromptText(raw string, maxChars int) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return `""`
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if maxChars > 0 && len(runes) > maxChars {
		text = string(runes[:maxChars]) + "..."
	}
	return fmt.Sprintf("%q", text)
}

func limitPromptLines(raw string, maxLines, maxChars int) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	runes := []rune(text)
	if maxChars > 0 && len(runes) > maxChars {
		text = string(runes[:maxChars]) + "..."
	}
	return text
}
