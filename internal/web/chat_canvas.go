package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type canvasBlock struct {
	Title   string
	Content string
}

type fileBlock struct {
	Path    string
	Content string
}

var canvasBlockRe = regexp.MustCompile(`(?s):::canvas\{([^}]*)\}\n?(.*?):::`)
var fileBlockRe = regexp.MustCompile(`(?s):::file\{([^}]*)\}\n?(.*?):::`)
var speakTagRe = regexp.MustCompile(`(?s)<speak(?:\s[^>]*)?>.*?</speak>`)

var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

type textSegment struct {
	text   string
	inCode bool
}

func parseCodeFencePrefix(line string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"));
	if len(trimmed) < 3 {
		return ""
	}
	lead := trimmed[0]
	if lead != '`' && lead != '~' {
		return ""
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == lead {
		i++
	}
	if i < 3 {
		return ""
	}
	return trimmed[:i]
}

func isCodeFenceClose(line, openMarker string) bool {
	marker := parseCodeFencePrefix(line)
	if marker == "" || openMarker == "" {
		return false
	}
	if marker[0] != openMarker[0] {
		return false
	}
	if len(marker) < len(openMarker) {
		return false
	}
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	trimmed = strings.TrimLeft(trimmed, " \t");
	if !strings.HasPrefix(trimmed, openMarker) {
		return false
	}
	suffix := strings.TrimLeft(trimmed[len(openMarker):], string(openMarker[0]))
	return strings.TrimSpace(suffix) == ""
}

func splitCodeSegments(text string) []textSegment {
	lines := strings.SplitAfter(text, "\n")
	segments := make([]textSegment, 0, len(lines))
	inCode := false
	openMarker := ""
	var current textSegment
	current.inCode = false
	for _, line := range lines {
		if inCode {
			current.text += line
			if isCodeFenceClose(line, openMarker) {
				segments = append(segments, current)
				current = textSegment{}
				inCode = false
				openMarker = ""
			}
			continue
		}
		if marker := parseCodeFencePrefix(line); marker != "" {
			if current.text != "" {
				segments = append(segments, current)
			}
			current = textSegment{inCode: true, text: line}
			inCode = true
			openMarker = marker
			continue
		}
		current.text += line
	}
	if current.text != "" {
		segments = append(segments, current)
	}
	if len(segments) == 0 {
		return []textSegment{{text: text, inCode: false}}
	}
	return segments
}

func parseCanvasBlocksInText(text string) ([]canvasBlock, string) {
	matches := canvasBlockRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}
	var blocks []canvasBlock
	cleaned := text
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		fullStart, fullEnd := m[0], m[1]
		attrsStart, attrsEnd := m[2], m[3]
		contentStart, contentEnd := m[4], m[5]

		attrs := text[attrsStart:attrsEnd]
		content := strings.TrimSpace(text[contentStart:contentEnd])
		title := extractAttr(attrs, "title")

		blocks = append([]canvasBlock{{
			Title:   title,
			Content: content,
		}}, blocks...)

		ref := fmt.Sprintf("[canvas: %s]", title)
		cleaned = cleaned[:fullStart] + ref + cleaned[fullEnd:]
	}
	return blocks, strings.TrimSpace(cleaned)
}

func extractAttr(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

func parseCanvasBlocks(text string) ([]canvasBlock, string) {
	segments := splitCodeSegments(text)
	var blocks []canvasBlock
	var cleaned []string
	for _, seg := range segments {
		if seg.inCode {
			cleaned = append(cleaned, seg.text)
			continue
		}
		segBlocks, segCleaned := parseCanvasBlocksInText(seg.text)
		blocks = append(blocks, segBlocks...)
		cleaned = append(cleaned, segCleaned)
	}
	return blocks, strings.TrimSpace(strings.Join(cleaned, ""))
}

func parseFileBlocks(text string) ([]fileBlock, string) {
	segments := splitCodeSegments(text)
	var blocks []fileBlock
	var cleaned []string
	for _, seg := range segments {
		if seg.inCode {
			cleaned = append(cleaned, seg.text)
			continue
		}
		matches := fileBlockRe.FindAllStringSubmatchIndex(seg.text, -1)
		if len(matches) == 0 {
			cleaned = append(cleaned, seg.text)
			continue
		}
		cleanedText := seg.text
		segBlocks := make([]fileBlock, 0)
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			fullStart, fullEnd := m[0], m[1]
			attrsStart, attrsEnd := m[2], m[3]
			contentStart, contentEnd := m[4], m[5]
			attrs := seg.text[attrsStart:attrsEnd]
			content := strings.TrimSpace(seg.text[contentStart:contentEnd])
			path := extractAttr(attrs, "path")
			segBlocks = append([]fileBlock{{
				Path:    path,
				Content: content,
			}}, segBlocks...)
			ref := fmt.Sprintf("[file: %s]", path)
			cleanedText = cleanedText[:fullStart] + ref + cleanedText[fullEnd:]
		}
		blocks = append(blocks, segBlocks...)
		cleaned = append(cleaned, cleanedText)
	}
	return blocks, strings.TrimSpace(strings.Join(cleaned, ""))
}

func stripSpeakTags(text string) string {
	return strings.TrimSpace(speakTagRe.ReplaceAllString(text, ""))
}

func (a *App) executeAssistantTextBlock(canvasSessionID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       canvasSessionID,
		"kind":             "text",
		"title":            "Assistant Response",
		"markdown_or_text": text,
	})
}

func (a *App) executeCanvasBlocks(canvasSessionID string, blocks []canvasBlock) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, block := range blocks {
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            block.Title,
			"markdown_or_text": block.Content,
		})
	}
}

func (a *App) executeFileBlocks(canvasSessionID string, blocks []fileBlock) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, block := range blocks {
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            block.Path,
			"markdown_or_text": block.Content,
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

// canvasFileTarget holds the resolved file-to-canvas binding for refresh.
type canvasFileTarget struct {
	sessionID string
	port      int
	title     string
	filePath  string
}

// resolveCanvasFileTarget resolves the active canvas artifact to a disk file.
// Returns nil if the artifact is not a text file or the title doesn't map to
// an existing file on disk.
func (a *App) resolveCanvasFileTarget(projectKey string) *canvasFileTarget {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil
	}
	sid := a.canvasSessionIDForProject(project)
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		return nil
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		return nil
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return nil
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind != "text_artifact" && kind != "text" {
		return nil
	}
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	if title == "" || title == "<nil>" {
		return nil
	}
	cwd := a.cwdForProjectKey(key)
	filePath := resolveArtifactFilePath(cwd, title)
	if filePath == "" {
		return nil
	}
	return &canvasFileTarget{sessionID: sid, port: port, title: title, filePath: filePath}
}

// refreshCanvasFromDisk does a single check: reads the file, compares with
// the canvas text, and pushes if different. Returns true if an update was pushed.
func (a *App) refreshCanvasFromDisk(projectKey string) bool {
	t := a.resolveCanvasFileTarget(projectKey)
	if t == nil {
		return false
	}
	return a.pushCanvasFileIfChanged(t)
}

func (a *App) pushCanvasFileIfChanged(t *canvasFileTarget) bool {
	diskBytes, err := os.ReadFile(t.filePath)
	if err != nil {
		return false
	}
	diskContent := string(diskBytes)
	status, err := a.mcpToolsCall(t.port, "canvas_status", map[string]interface{}{"session_id": t.sessionID})
	if err != nil {
		return false
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return false
	}
	currentText, _ := active["text"].(string)
	if strings.TrimSpace(diskContent) == strings.TrimSpace(currentText) {
		return false
	}
	_, _ = a.mcpToolsCall(t.port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       t.sessionID,
		"kind":             "text",
		"title":            t.title,
		"markdown_or_text": diskContent,
	})
	return true
}

// watchCanvasFile uses fsnotify to watch the disk file backing the active
// canvas artifact. On every write, it reads the new content and pushes it
// to the canvas via MCP. Blocks until ctx is cancelled.
func (a *App) watchCanvasFile(ctx context.Context, projectKey string) {
	t := a.resolveCanvasFileTarget(projectKey)
	if t == nil {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	dir := filepath.Dir(t.filePath)
	if err := watcher.Add(dir); err != nil {
		return
	}
	base := filepath.Base(t.filePath)
	lastContent := ""
	if b, err := os.ReadFile(t.filePath); err == nil {
		lastContent = string(b)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			b, err := os.ReadFile(t.filePath)
			if err != nil {
				continue
			}
			content := string(b)
			if content == lastContent {
				continue
			}
			lastContent = content
			_, _ = a.mcpToolsCall(t.port, "canvas_artifact_show", map[string]interface{}{
				"session_id":       t.sessionID,
				"kind":             "text",
				"title":            t.title,
				"markdown_or_text": content,
			})
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
