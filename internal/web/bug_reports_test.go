package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5W8xkAAAAASUVORK5CYII="

func TestHandleBugReportCreateWritesBundleUnderWorkspaceArtifacts(t *testing.T) {
	app := newAuthedTestApp(t)
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	workspace, err := app.store.CreateWorkspace("Tabura", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"trigger":             "button",
		"timestamp":           "2026-03-08T15:04:05Z",
		"page_url":            "http://127.0.0.1:8420/",
		"version":             "0.1.8",
		"boot_id":             "boot-123",
		"started_at":          "2026-03-08T14:00:00Z",
		"active_mode":         "pen",
		"canvas_state":        map[string]any{"has_artifact": true, "artifact_title": "README.md"},
		"recent_events":       []string{"tap at (12,18)", "report bug"},
		"browser_logs":        []string{"warn: render slow"},
		"device":              map[string]any{"ua": "Playwright", "viewport": "1280x720"},
		"note":                "The indicator froze after the tap.",
		"voice_transcript":    "it stops responding after the second tap",
		"screenshot_data_url": testPNGDataURL,
		"annotated_data_url":  testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	bundlePath := strFromAny(payload["bundle_path"])
	screenshotPath := strFromAny(payload["screenshot_path"])
	annotatedPath := strFromAny(payload["annotated_path"])
	if !strings.HasPrefix(bundlePath, ".tabura/artifacts/bugs/") {
		t.Fatalf("bundle_path = %q, want .tabura/artifacts/bugs/... path", bundlePath)
	}
	if !strings.HasSuffix(screenshotPath, "screenshot.png") {
		t.Fatalf("screenshot_path = %q, want screenshot.png suffix", screenshotPath)
	}
	if !strings.HasSuffix(annotatedPath, "annotated.png") {
		t.Fatalf("annotated_path = %q, want annotated.png suffix", annotatedPath)
	}
	bundleBytes, err := os.ReadFile(filepath.Join(workspaceDir, filepath.FromSlash(bundlePath)))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got := strFromAny(bundle["active_workspace"]); got != "Tabura" {
		t.Fatalf("active_workspace = %q, want %q", got, "Tabura")
	}
	if got := strFromAny(bundle["active_mode"]); got != "pen" {
		t.Fatalf("active_mode = %q, want %q", got, "pen")
	}
	if got := strFromAny(bundle["note"]); got != "The indicator froze after the tap." {
		t.Fatalf("note = %q, want note to round-trip", got)
	}
	if got := strFromAny(bundle["voice_transcript"]); got != "it stops responding after the second tap" {
		t.Fatalf("voice_transcript = %q, want transcript to round-trip", got)
	}
	if got := strFromAny(bundle["screenshot"]); got != screenshotPath {
		t.Fatalf("bundle screenshot = %q, want %q", got, screenshotPath)
	}
	if got := strFromAny(bundle["annotated_image"]); got != annotatedPath {
		t.Fatalf("bundle annotated_image = %q, want %q", got, annotatedPath)
	}
	if got := strFromAny(bundle["git_sha"]); !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(got) {
		t.Fatalf("git_sha = %q, want 40 hex chars", got)
	}
	canvasState, ok := bundle["canvas_state"].(map[string]any)
	if !ok {
		t.Fatalf("canvas_state = %#v, want object", bundle["canvas_state"])
	}
	if got := strFromAny(canvasState["artifact_title"]); got != "README.md" {
		t.Fatalf("canvas_state.artifact_title = %q, want %q", got, "README.md")
	}
}

func TestHandleBugReportCreateRequiresWorkspaceContext(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 409 {
		t.Fatalf("POST /api/bugs/report status = %d, want 409: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "active workspace or local project") {
		t.Fatalf("POST /api/bugs/report body = %q, want workspace context error", rr.Body.String())
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "tabura@example.com"},
		{"git", "config", "user.name", "Tabura Test"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, string(out))
		}
	}
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	commitCommands := [][]string{
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "init"},
	}
	for _, args := range commitCommands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, string(out))
		}
	}
}
