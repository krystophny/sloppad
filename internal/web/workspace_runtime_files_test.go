package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestProjectFilesListReturnsOneLevelAndSupportsSubfolders(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	root := filepath.Clean(project.RootPath)
	dirName := "zz_test_dir"
	fileName := "zz_test_file.txt"
	dirPath := filepath.Join(root, dirName)
	filePath := filepath.Join(root, fileName)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir test dir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("root"), 0o644); err != nil {
		t.Fatalf("write root test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "child.md"), []byte("child"), 0o644); err != nil {
		t.Fatalf("write child test file: %v", err)
	}

	rrRoot := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/workspaces/"+itoa(workspace.ID)+"/files",
		nil,
	)
	if rrRoot.Code != http.StatusOK {
		t.Fatalf("expected root list 200, got %d: %s", rrRoot.Code, rrRoot.Body.String())
	}
	var rootPayload workspaceFilesListResponse
	if err := json.Unmarshal(rrRoot.Body.Bytes(), &rootPayload); err != nil {
		t.Fatalf("decode root payload: %v", err)
	}
	if !rootPayload.OK {
		t.Fatalf("expected ok=true")
	}
	if rootPayload.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %d, want %d", rootPayload.WorkspaceID, workspace.ID)
	}
	if !rootPayload.IsRoot || rootPayload.Path != "" {
		t.Fatalf("expected root listing, got is_root=%v path=%q", rootPayload.IsRoot, rootPayload.Path)
	}
	dirIndex := -1
	fileIndex := -1
	for i, entry := range rootPayload.Entries {
		if entry.Path == dirName {
			if !entry.IsDir {
				t.Fatalf("expected %q to be a directory", dirName)
			}
			dirIndex = i
		}
		if entry.Path == fileName {
			if entry.IsDir {
				t.Fatalf("expected %q to be a file", fileName)
			}
			fileIndex = i
		}
	}
	if dirIndex < 0 || fileIndex < 0 {
		t.Fatalf("expected seeded entries in root listing")
	}
	if dirIndex > fileIndex {
		t.Fatalf("expected directories before files in listing")
	}

	rrSub := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/workspaces/"+itoa(workspace.ID)+"/files?path="+dirName,
		nil,
	)
	if rrSub.Code != http.StatusOK {
		t.Fatalf("expected subdirectory list 200, got %d: %s", rrSub.Code, rrSub.Body.String())
	}
	var subPayload workspaceFilesListResponse
	if err := json.Unmarshal(rrSub.Body.Bytes(), &subPayload); err != nil {
		t.Fatalf("decode sub payload: %v", err)
	}
	if subPayload.IsRoot || subPayload.Path != dirName {
		t.Fatalf("expected subdirectory payload for %q, got is_root=%v path=%q", dirName, subPayload.IsRoot, subPayload.Path)
	}
	if len(subPayload.Entries) == 0 || subPayload.Entries[0].Path != dirName+"/child.md" {
		t.Fatalf("expected child file path %q in subdirectory listing", dirName+"/child.md")
	}
}

func TestProjectFilesListBlocksWorkPersonalSubtree(t *testing.T) {
	vaultRoot, personalRoot := configureWorkPersonalGuardrail(t)
	app := newAuthedTestApp(t)
	if err := os.WriteFile(filepath.Join(personalRoot, "diary.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write protected file: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Work Vault", vaultRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(vault) error: %v", err)
	}

	rrRoot := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/files", nil)
	if rrRoot.Code != http.StatusOK {
		t.Fatalf("root list status = %d, want 200: %s", rrRoot.Code, rrRoot.Body.String())
	}
	body := rrRoot.Body.String()
	if strings.Contains(body, "personal") || strings.Contains(body, "diary.md") {
		t.Fatalf("root listing leaked protected subtree metadata: %s", body)
	}

	rrPersonal := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/files?path=personal", nil)
	if rrPersonal.Code != http.StatusForbidden {
		t.Fatalf("personal list status = %d, want 403: %s", rrPersonal.Code, rrPersonal.Body.String())
	}
	body = rrPersonal.Body.String()
	if !strings.Contains(body, "work personal subtree is blocked") {
		t.Fatalf("personal list response = %q", body)
	}
	if strings.Contains(body, "diary.md") || strings.Contains(body, personalRoot) {
		t.Fatalf("personal list response leaked protected metadata: %q", body)
	}
}

func TestProjectFilesListRejectsTraversal(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/workspaces/"+itoa(workspace.ID)+"/files?path=../secret",
		nil,
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected traversal request 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProjectWelcomeListsDocsAndRecentFiles(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var listPayload workspacesListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(listPayload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	workspaceID := listPayload.Projects[0].ID
	project, err := app.store.GetEnrichedWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "notes.txt"), []byte("recent"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	rrWelcome := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/"+workspaceID+"/welcome", nil)
	if rrWelcome.Code != http.StatusOK {
		t.Fatalf("expected welcome 200, got %d: %s", rrWelcome.Code, rrWelcome.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rrWelcome.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode welcome response: %v", err)
	}
	if got := strFromAny(payload["scope"]); got != "project" {
		t.Fatalf("scope = %q, want %q", got, "project")
	}
	sections, ok := payload["sections"].([]any)
	if !ok || len(sections) == 0 {
		t.Fatalf("expected welcome sections, got %v", payload["sections"])
	}
	body := rrWelcome.Body.String()
	if !strings.Contains(body, "README.md") {
		t.Fatalf("welcome body missing README.md: %s", body)
	}
	if !strings.Contains(body, "notes.txt") {
		t.Fatalf("welcome body missing notes.txt: %s", body)
	}
}

func TestProjectWelcomeIncludesRuntimeCards(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var listPayload workspacesListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(listPayload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	workspaceID := listPayload.Projects[0].ID

	rrWelcome := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/"+workspaceID+"/welcome", nil)
	if rrWelcome.Code != http.StatusOK {
		t.Fatalf("expected project welcome 200, got %d: %s", rrWelcome.Code, rrWelcome.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rrWelcome.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode welcome response: %v", err)
	}
	if got := strFromAny(payload["scope"]); got != "project" {
		t.Fatalf("scope = %q, want %q", got, "project")
	}
	if !strings.Contains(rrWelcome.Body.String(), "Silent mode") {
		t.Fatalf("welcome missing runtime card: %s", rrWelcome.Body.String())
	}
}
