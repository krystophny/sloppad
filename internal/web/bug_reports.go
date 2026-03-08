package web

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const taburaVersion = "0.1.8"

type bugReportRequest struct {
	Trigger          string          `json:"trigger"`
	Timestamp        string          `json:"timestamp"`
	PageURL          string          `json:"page_url"`
	Version          string          `json:"version"`
	BootID           string          `json:"boot_id"`
	StartedAt        string          `json:"started_at"`
	ActiveMode       string          `json:"active_mode"`
	CanvasState      json.RawMessage `json:"canvas_state"`
	RecentEvents     []string        `json:"recent_events"`
	BrowserLogs      []string        `json:"browser_logs"`
	Device           map[string]any  `json:"device"`
	Note             string          `json:"note"`
	VoiceTranscript  string          `json:"voice_transcript"`
	ScreenshotData   string          `json:"screenshot_data_url"`
	AnnotatedDataURL string          `json:"annotated_data_url"`
}

type bugReportBundle struct {
	Trigger          string          `json:"trigger"`
	Timestamp        string          `json:"timestamp"`
	PageURL          string          `json:"page_url,omitempty"`
	Version          string          `json:"version"`
	BootID           string          `json:"boot_id,omitempty"`
	StartedAt        string          `json:"started_at,omitempty"`
	GitSHA           string          `json:"git_sha,omitempty"`
	ActiveMode       string          `json:"active_mode,omitempty"`
	ActiveWorkspace  string          `json:"active_workspace,omitempty"`
	ActiveSphere     string          `json:"active_sphere,omitempty"`
	CanvasState      json.RawMessage `json:"canvas_state,omitempty"`
	RecentEvents     []string        `json:"recent_events,omitempty"`
	BrowserLogs      []string        `json:"browser_logs,omitempty"`
	Device           map[string]any  `json:"device,omitempty"`
	Note             string          `json:"note,omitempty"`
	VoiceTranscript  string          `json:"voice_transcript,omitempty"`
	ScreenshotPath   string          `json:"screenshot,omitempty"`
	AnnotatedPath    string          `json:"annotated_image,omitempty"`
	WorkspaceDirPath string          `json:"workspace_dir_path,omitempty"`
}

type bugReportFile struct {
	bytes []byte
	ext   string
}

func (a *App) handleBugReportCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req bugReportRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	screenshot, err := decodeBugReportDataURL(req.ScreenshotData)
	if err != nil {
		http.Error(w, "screenshot_data_url must be a valid PNG or JPEG data URL", http.StatusBadRequest)
		return
	}
	var annotated *bugReportFile
	if strings.TrimSpace(req.AnnotatedDataURL) != "" {
		file, err := decodeBugReportDataURL(req.AnnotatedDataURL)
		if err != nil {
			http.Error(w, "annotated_data_url must be a valid PNG or JPEG data URL", http.StatusBadRequest)
			return
		}
		annotated = &file
	}
	workspaceName, workspaceDir, err := a.resolveBugReportWorkspace()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	reportDir, reportID, err := createBugReportDir(workspaceDir, req.Timestamp)
	if err != nil {
		http.Error(w, "create bug report dir failed", http.StatusInternalServerError)
		return
	}
	screenshotPath := filepath.Join(reportDir, "screenshot"+screenshot.ext)
	if err := os.WriteFile(screenshotPath, screenshot.bytes, 0o644); err != nil {
		http.Error(w, "write screenshot failed", http.StatusInternalServerError)
		return
	}
	var annotatedPath string
	if annotated != nil {
		annotatedPath = filepath.Join(reportDir, "annotated"+annotated.ext)
		if err := os.WriteFile(annotatedPath, annotated.bytes, 0o644); err != nil {
			http.Error(w, "write annotated image failed", http.StatusInternalServerError)
			return
		}
	}
	timestamp := normalizeBugReportTimestamp(req.Timestamp)
	bundle := bugReportBundle{
		Trigger:          strings.TrimSpace(req.Trigger),
		Timestamp:        timestamp,
		PageURL:          strings.TrimSpace(req.PageURL),
		Version:          firstNonEmpty(strings.TrimSpace(req.Version), taburaVersion),
		BootID:           strings.TrimSpace(req.BootID),
		StartedAt:        strings.TrimSpace(req.StartedAt),
		GitSHA:           resolveGitSHA(workspaceDir),
		ActiveMode:       strings.TrimSpace(req.ActiveMode),
		ActiveWorkspace:  workspaceName,
		ActiveSphere:     "",
		CanvasState:      normalizeBugReportRawJSON(req.CanvasState),
		RecentEvents:     cleanBugReportLines(req.RecentEvents),
		BrowserLogs:      cleanBugReportLines(req.BrowserLogs),
		Device:           req.Device,
		Note:             strings.TrimSpace(req.Note),
		VoiceTranscript:  strings.TrimSpace(req.VoiceTranscript),
		ScreenshotPath:   toBugReportRelativePath(workspaceDir, screenshotPath),
		AnnotatedPath:    toBugReportRelativePath(workspaceDir, annotatedPath),
		WorkspaceDirPath: workspaceDir,
	}
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		http.Error(w, "encode bundle failed", http.StatusInternalServerError)
		return
	}
	bundlePath := filepath.Join(reportDir, "bundle.json")
	if err := os.WriteFile(bundlePath, bundleJSON, 0o644); err != nil {
		http.Error(w, "write bundle failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"ok":              true,
		"report_id":       reportID,
		"bundle_path":     toBugReportRelativePath(workspaceDir, bundlePath),
		"screenshot_path": bundle.ScreenshotPath,
		"annotated_path":  bundle.AnnotatedPath,
		"workspace":       workspaceName,
		"git_sha":         bundle.GitSHA,
	})
}

func (a *App) resolveBugReportWorkspace() (string, string, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return "", "", err
	}
	for _, workspace := range workspaces {
		if workspace.IsActive {
			return workspace.Name, workspace.DirPath, nil
		}
	}
	if root := strings.TrimSpace(a.localProjectDir); root != "" {
		name := filepath.Base(root)
		if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
			name = "local"
		}
		return name, root, nil
	}
	return "", "", errors.New("bug report requires an active workspace or local project")
}

func decodeBugReportDataURL(raw string) (bugReportFile, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return bugReportFile{}, errors.New("missing data URL")
	}
	comma := strings.IndexByte(clean, ',')
	if comma <= 0 {
		return bugReportFile{}, errors.New("invalid data URL")
	}
	header := clean[:comma]
	payload := clean[comma+1:]
	if !strings.HasPrefix(strings.ToLower(header), "data:image/") || !strings.Contains(strings.ToLower(header), ";base64") {
		return bugReportFile{}, errors.New("unsupported data URL")
	}
	var ext string
	switch {
	case strings.HasPrefix(strings.ToLower(header), "data:image/png"):
		ext = ".png"
	case strings.HasPrefix(strings.ToLower(header), "data:image/jpeg"), strings.HasPrefix(strings.ToLower(header), "data:image/jpg"):
		ext = ".jpg"
	default:
		return bugReportFile{}, errors.New("unsupported image type")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return bugReportFile{}, err
	}
	if len(decoded) == 0 {
		return bugReportFile{}, errors.New("empty image")
	}
	return bugReportFile{bytes: decoded, ext: ext}, nil
}

func normalizeBugReportTimestamp(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	if parsed, err := time.Parse(time.RFC3339, clean); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return clean
}

func normalizeBugReportRawJSON(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

func cleanBugReportLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func createBugReportDir(workspaceDir, rawTimestamp string) (string, string, error) {
	timestamp := normalizeBugReportTimestamp(rawTimestamp)
	stamp := strings.NewReplacer(":", "", "-", "", ".", "").Replace(timestamp)
	stamp = strings.TrimSuffix(stamp, "Z")
	stamp = strings.ReplaceAll(stamp, "T", "-")
	if stamp == "" {
		stamp = time.Now().UTC().Format("20060102-150405")
	}
	suffix, err := randomBugReportSuffix()
	if err != nil {
		return "", "", err
	}
	reportID := stamp + "-" + suffix
	dir := filepath.Join(workspaceDir, ".tabura", "artifacts", "bugs", reportID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return dir, reportID, nil
}

func randomBugReportSuffix() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func toBugReportRelativePath(workspaceDir, fullPath string) string {
	clean := strings.TrimSpace(fullPath)
	if clean == "" {
		return ""
	}
	rel, err := filepath.Rel(workspaceDir, clean)
	if err != nil {
		return filepath.ToSlash(clean)
	}
	return filepath.ToSlash(rel)
}

func resolveGitSHA(dir string) string {
	clean := strings.TrimSpace(dir)
	if clean == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", clean, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}
