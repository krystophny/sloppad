package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestWorkspaceMarkdownLinkResolveAllowsBrainAndVaultLinks(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	sourceRoot := filepath.Join(vaultRoot, "sources")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir brain topics: %v", err)
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir sources: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("[related](related.md)"), 0o644); err != nil {
		t.Fatalf("write active note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "related.md"), []byte("related"), 0o644); err != nil {
		t.Fatalf("write related note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "paper.md"), []byte("paper"), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	inBrain := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "related.md", "")
	if !inBrain.OK || inBrain.ResolvedPath != "brain/topics/related.md" || inBrain.Kind != "text" {
		t.Fatalf("in-brain resolution = %+v", inBrain)
	}
	outToSource := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../sources/paper.md", "")
	if !outToSource.OK || outToSource.ResolvedPath != "sources/paper.md" || outToSource.Kind != "text" {
		t.Fatalf("out-to-source resolution = %+v", outToSource)
	}
	if strings.Contains(outToSource.FileURL, vaultRoot) || filepath.IsAbs(outToSource.ResolvedPath) {
		t.Fatalf("resolution leaked absolute path: %+v", outToSource)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, outToSource.FileURL, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("linked file status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if strings.TrimSpace(rr.Body.String()) != "paper" {
		t.Fatalf("linked file body = %q, want paper", rr.Body.String())
	}
}

func TestWorkspaceMarkdownLinkResolveSupportsWikilinks(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir brain topics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("[[Related Note]]"), 0o644); err != nil {
		t.Fatalf("write active note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "Related Note.md"), []byte("related"), 0o644); err != nil {
		t.Fatalf("write related note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	resolved := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "Related Note", "wikilink")
	if !resolved.OK || resolved.ResolvedPath != "brain/topics/Related Note.md" {
		t.Fatalf("wikilink resolution = %+v", resolved)
	}
}

func TestWorkspaceMarkdownLinkResolveRejectsOutOfVaultAndWorkPersonal(t *testing.T) {
	vaultRoot, personalRoot := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	outsidePath := filepath.Join(filepath.Dir(vaultRoot), "outside.md")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir brain topics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write active note: %v", err)
	}
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personalRoot, "diary.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write personal note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	outside := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../../outside.md", "")
	if outside.OK || !outside.Blocked || !strings.Contains(outside.Reason, "leaves the vault") {
		t.Fatalf("out-of-vault resolution = %+v", outside)
	}
	personal := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../personal/diary.md", "")
	if personal.OK || !personal.Blocked || !strings.Contains(personal.Reason, "work personal subtree is blocked") {
		t.Fatalf("personal resolution = %+v", personal)
	}
	if strings.Contains(personal.Reason, personalRoot) || strings.Contains(outside.Reason, outsidePath) {
		t.Fatalf("blocked reason leaked absolute path: personal=%q outside=%q", personal.Reason, outside.Reason)
	}
}

func TestWorkspaceMarkdownLinkResolveOpensLinkedFolderWithinVault(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	sourceDir := filepath.Join(brainRoot, "topics")
	targetDir := filepath.Join(vaultRoot, "project", "path")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	resolution := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../project/path/", "")
	wantRel := filepath.ToSlash(filepath.Join("project", "path"))
	if !resolution.OK {
		t.Fatalf("resolution blocked: %+v", resolution)
	}
	if resolution.Kind != "folder" {
		t.Fatalf("resolution kind = %q, want folder", resolution.Kind)
	}
	if resolution.FileURL != "" {
		t.Fatalf("folder resolution file_url = %q, want empty", resolution.FileURL)
	}
	if resolution.ResolvedPath != wantRel || resolution.VaultRelativePath != wantRel {
		t.Fatalf("resolution path = %+v, want relative %q", resolution, wantRel)
	}
	if strings.HasPrefix(resolution.ResolvedPath, string(filepath.Separator)) || strings.Contains(resolution.ResolvedPath, ":") {
		t.Fatalf("resolution leaked absolute path: %+v", resolution)
	}
}

func TestWorkspaceMarkdownLinkResolveUsesRelativeFileURLForVaultNotes(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	sourceDir := filepath.Join(brainRoot, "topics")
	targetDir := filepath.Join(vaultRoot, "project", "path")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "file.md"), []byte("target"), 0o644); err != nil {
		t.Fatalf("write target note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	resolution := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../project/path/file.md", "")
	wantRel := filepath.ToSlash(filepath.Join("project", "path", "file.md"))
	if !resolution.OK {
		t.Fatalf("resolution blocked: %+v", resolution)
	}
	if resolution.Kind != "text" {
		t.Fatalf("resolution kind = %q, want text", resolution.Kind)
	}
	if resolution.ResolvedPath != wantRel || resolution.VaultRelativePath != wantRel {
		t.Fatalf("resolution path = %+v, want relative %q", resolution, wantRel)
	}
	if resolution.FileURL == "" || !strings.Contains(resolution.FileURL, "/api/workspaces/") {
		t.Fatalf("resolution file_url = %q, want api path", resolution.FileURL)
	}
	if strings.Contains(resolution.FileURL, "file://") || strings.Contains(resolution.FileURL, vaultRoot) || strings.Contains(resolution.FileURL, string(filepath.Separator)+"home"+string(filepath.Separator)) {
		t.Fatalf("resolution leaked machine path in file_url: %+v", resolution)
	}
}

func TestWorkspaceMarkdownLinkResolveReportsMissingTarget(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	sourceDir := filepath.Join(brainRoot, "topics")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}

	resolution := resolveMarkdownLinkForTest(t, app, workspace.ID, "topics/active.md", "../../project/path/missing.md", "")
	if resolution.OK || !resolution.Blocked || resolution.Reason != "link target was not found in the vault" {
		t.Fatalf("missing target resolution = %+v", resolution)
	}
}

func resolveMarkdownLinkForTest(t *testing.T, app *App, workspaceID int64, sourcePath, target, linkType string) workspaceMarkdownLinkResolution {
	t.Helper()
	values := url.Values{}
	values.Set("source", sourcePath)
	values.Set("target", target)
	if linkType != "" {
		values.Set("type", linkType)
	}
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/workspaces/"+itoa(workspaceID)+"/markdown-link/resolve?"+values.Encode(),
		nil,
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload workspaceMarkdownLinkResolution
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode resolution: %v", err)
	}
	return payload
}
