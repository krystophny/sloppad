package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

type workspacesListResponse struct {
	OK                 bool                 `json:"ok"`
	DefaultWorkspaceID string               `json:"default_workspace_id"`
	ActiveWorkspaceID  string               `json:"active_workspace_id"`
	Projects           []workspaceListEntry `json:"workspaces"`
}

type workspaceListEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Sphere          string `json:"sphere"`
	WorkspacePath   string `json:"workspace_path"`
	ChatSessionID   string `json:"chat_session_id"`
	ChatModel       string `json:"chat_model"`
	ReasoningEffort string `json:"chat_model_reasoning_effort"`
	CanvasSessionID string `json:"canvas_session_id"`
	Unread          bool   `json:"unread"`
	ReviewPending   bool   `json:"review_pending"`
	RunState        struct {
		ActiveTurns  int    `json:"active_turns"`
		QueuedTurns  int    `json:"queued_turns"`
		IsWorking    bool   `json:"is_working"`
		Status       string `json:"status"`
		ActiveTurnID string `json:"active_turn_id"`
	} `json:"run_state"`
}

type workspacesActivityResponse struct {
	OK       bool `json:"ok"`
	Projects []struct {
		WorkspaceID   string `json:"workspace_id"`
		ChatSessionID string `json:"chat_session_id"`
		ChatMode      string `json:"chat_mode"`
		Unread        bool   `json:"unread"`
		ReviewPending bool   `json:"review_pending"`
		RunState      struct {
			ActiveTurns int    `json:"active_turns"`
			QueuedTurns int    `json:"queued_turns"`
			IsWorking   bool   `json:"is_working"`
			Status      string `json:"status"`
		} `json:"run_state"`
	} `json:"workspaces"`
}

type workspaceFilesListResponse struct {
	OK          bool   `json:"ok"`
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
	IsRoot      bool   `json:"is_root"`
	Entries     []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	} `json:"entries"`
}

func configureWorkPersonalGuardrail(t *testing.T) (string, string) {
	t.Helper()
	vaultRoot := t.TempDir()
	brainRoot := filepath.Join(vaultRoot, "brain")
	personalRoot := filepath.Join(vaultRoot, "personal")
	if err := os.MkdirAll(brainRoot, 0o755); err != nil {
		t.Fatalf("mkdir brain root: %v", err)
	}
	if err := os.MkdirAll(personalRoot, 0o755); err != nil {
		t.Fatalf("mkdir personal root: %v", err)
	}
	t.Setenv("SLOPSHELL_BRAIN_WORK_ROOT", brainRoot)
	return vaultRoot, personalRoot
}

func findWorkspaceByName(projects []workspaceListEntry, name string) *workspaceListEntry {
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i]
		}
	}
	return nil
}

func TestProjectsListIncludesActiveAndSessions(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("expected ok=true")
	}
	if payload.DefaultWorkspaceID == "" {
		t.Fatalf("expected default workspace id")
	}
	if payload.ActiveWorkspaceID == "" {
		t.Fatalf("expected active workspace id")
	}
	if len(payload.Projects) == 0 {
		t.Fatalf("expected at least one workspace")
	}
	first := payload.Projects[0]
	if first.ChatSessionID == "" {
		t.Fatalf("expected workspace chat session id")
	}
	if first.CanvasSessionID == "" {
		t.Fatalf("expected workspace canvas session id")
	}
	if first.ChatModel == "" {
		t.Fatalf("expected workspace chat model")
	}
	if first.RunState.Status == "" {
		t.Fatalf("expected workspace run state status")
	}
}

func TestNewAppPrefersLocalProjectWorkspaceOnStartup(t *testing.T) {
	dataDir := t.TempDir()
	localProjectDir := filepath.Join(t.TempDir(), "tabula")
	if err := os.MkdirAll(localProjectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(localProjectDir) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localProjectDir, "go.mod"), []byte("module github.com/sloppy-org/slopshell\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error: %v", err)
	}

	app, err := New(dataDir, localProjectDir, "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace() error: %v", err)
	}
	if project.Name != "Slopshell" {
		t.Fatalf("default project name = %q, want %q", project.Name, "Slopshell")
	}
	if err := app.ensureStartupProjectWithWorkspace(); err != nil {
		t.Fatalf("ensureStartupProjectWithWorkspace() error: %v", err)
	}
	workspace, err := app.store.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() error: %v", err)
	}
	if workspace.IsDaily {
		t.Fatal("active workspace is_daily = true, want false for local workspace startup")
	}
	if workspace.DirPath != localProjectDir {
		t.Fatalf("active workspace dir_path = %q, want %q", workspace.DirPath, localProjectDir)
	}
	activeWorkspaceID, err := app.store.ActiveWorkspaceID()
	if err != nil {
		t.Fatalf("ActiveWorkspaceID() error: %v", err)
	}
	if activeWorkspaceID != workspaceIDStr(project.ID) {
		t.Fatalf("active project id = %q, want %q", activeWorkspaceID, workspaceIDStr(project.ID))
	}
	workspace, err = app.resolveChatSessionTarget("", nil)
	if err != nil {
		t.Fatalf("resolveChatSessionTarget() error: %v", err)
	}
	if workspace.DirPath != localProjectDir {
		t.Fatalf("resolved workspace dir_path = %q, want %q", workspace.DirPath, localProjectDir)
	}
}

func TestStartupWorkspacePrefersBrainPresetOverLocalProject(t *testing.T) {
	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "data")
	localProjectDir := filepath.Join(rootDir, "tabula")
	workVault := filepath.Join(rootDir, "work-vault")
	brainRoot := filepath.Join(workVault, "brain")
	if err := os.MkdirAll(localProjectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(localProjectDir) error: %v", err)
	}
	if err := os.MkdirAll(brainRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(brainRoot) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localProjectDir, "go.mod"), []byte("module github.com/sloppy-org/slopshell\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error: %v", err)
	}
	configPath := filepath.Join(rootDir, "vaults.toml")
	config := `[[vault]]
sphere = "work"
root = "` + workVault + `"
brain = "brain"
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile(vault config) error: %v", err)
	}
	t.Setenv("SLOPTOOLS_VAULT_CONFIG", configPath)

	app, err := New(dataDir, localProjectDir, "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := app.store.AddAuthSession(testAuthToken); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	localWorkspace, err := app.store.CreateWorkspace("Default Workspace", localProjectDir, store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(local) error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(localWorkspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(local) error: %v", err)
	}

	startupWorkspace, err := app.ensureStartupWorkspace()
	if err != nil {
		t.Fatalf("ensureStartupWorkspace() error: %v", err)
	}
	if startupWorkspace.DirPath != brainRoot {
		t.Fatalf("startup dir_path = %q, want %q", startupWorkspace.DirPath, brainRoot)
	}
	if startupWorkspace.Name != "Work brain" {
		t.Fatalf("startup name = %q, want %q", startupWorkspace.Name, "Work brain")
	}
	activeWorkspace, err := app.store.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() error: %v", err)
	}
	if activeWorkspace.DirPath != brainRoot {
		t.Fatalf("active workspace dir_path = %q, want %q", activeWorkspace.DirPath, brainRoot)
	}
}

func TestWorkPersonalGuardrailUsesBrainConfigGetRoot(t *testing.T) {
	rootDir := t.TempDir()
	workVault := filepath.Join(rootDir, "work-vault")
	privateVault := filepath.Join(rootDir, "private-vault")
	workBrainRoot := filepath.Join(workVault, "brain")
	workPersonalRoot := filepath.Join(workVault, "personal")
	if err := os.MkdirAll(workBrainRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workBrainRoot): %v", err)
	}
	if err := os.MkdirAll(workPersonalRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workPersonalRoot): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(privateVault, "brain"), 0o755); err != nil {
		t.Fatalf("MkdirAll(private brain): %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"structuredContent": map[string]any{
					"vaults": []any{
						map[string]any{"sphere": "work", "root": workVault, "brain": "brain"},
						map[string]any{"sphere": "private", "root": privateVault, "brain": "brain"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	t.Setenv("SLOPSHELL_BRAIN_WORK_ROOT", filepath.Join(rootDir, "env-work", "brain"))
	t.Setenv("SLOPSHELL_BRAIN_PRIVATE_ROOT", filepath.Join(rootDir, "env-private", "brain"))

	app := newAuthedTestApp(t)
	app.tunnels.setEndpoint(LocalSessionID, mcpEndpoint{httpURL: server.URL})

	if !pathInWorkPersonalGuardrail(filepath.Join(workPersonalRoot, "diary.md")) {
		t.Fatalf("expected configured work personal subtree to be blocked")
	}
	if pathInWorkPersonalGuardrail(filepath.Join(rootDir, "env-work", "personal", "diary.md")) {
		t.Fatalf("expected env fallback to be ignored when brain.config.get is available")
	}
}

func TestProjectAPIModelIncludesWorkspaceChatSession(t *testing.T) {
	app := newAuthedTestApp(t)

	defaultProject, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if _, err := app.ensureWorkspaceReady(defaultProject, false); err != nil {
		t.Fatalf("ensure workspace for default project: %v", err)
	}
	defaultItem, err := app.buildWorkspaceAPIModel(defaultProject)
	if err != nil {
		t.Fatalf("build default project API model: %v", err)
	}
	if defaultItem.ChatSessionID == "" {
		t.Fatalf("expected project chat session id")
	}
}

func TestProjectsListIncludesWorkspaceSphere(t *testing.T) {
	app := newAuthedTestApp(t)

	workspaceRoot := filepath.Join(t.TempDir(), "work-root")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceRoot) error: %v", err)
	}
	if _, err := app.store.CreateWorkspace("Work Root", workspaceRoot, store.SphereWork); err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	projectPath := filepath.Join(workspaceRoot, "linked-project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectPath) error: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"name": "linked-work-project",
		"kind": "linked",
		"path": projectPath,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create project status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("list projects status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	project := findWorkspaceByName(payload.Projects, "linked-work-project")
	if project == nil {
		t.Fatalf("linked project not found in payload: %#v", payload.Projects)
	}
	if project.Sphere != store.SphereWork {
		t.Fatalf("project sphere = %q, want %q", project.Sphere, store.SphereWork)
	}
}

func TestRuntimeWorkspaceCreateRejectsWorkPersonalPath(t *testing.T) {
	_, personalRoot := configureWorkPersonalGuardrail(t)
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"name": "Personal",
		"kind": "linked",
		"path": personalRoot,
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("create personal workspace status = %d, want 403: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "work personal subtree is blocked") {
		t.Fatalf("guardrail response = %q", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), personalRoot) {
		t.Fatalf("guardrail response leaked protected path: %q", rr.Body.String())
	}
	if _, err := app.store.GetWorkspaceByPath(personalRoot); !isNoRows(err) {
		t.Fatalf("GetWorkspaceByPath(personal) error = %v, want no rows", err)
	}
}

func TestProjectsListPrefersLastUsedWorkspaceProject(t *testing.T) {
	app := newAuthedTestApp(t)

	alphaPath := filepath.Join(t.TempDir(), "alpha")
	betaPath := filepath.Join(t.TempDir(), "beta")
	if err := os.MkdirAll(alphaPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(alpha) error: %v", err)
	}
	if err := os.MkdirAll(betaPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(beta) error: %v", err)
	}
	alphaProject, _, err := app.createWorkspace2(runtimeWorkspaceCreateRequest{Name: "Alpha", Kind: "linked", Path: alphaPath})
	if err != nil {
		t.Fatalf("createWorkspace2(alpha) error: %v", err)
	}
	betaProject, _, err := app.createWorkspace2(runtimeWorkspaceCreateRequest{Name: "Beta", Kind: "linked", Path: betaPath})
	if err != nil {
		t.Fatalf("createWorkspace2(beta) error: %v", err)
	}
	alphaWorkspace, err := app.ensureWorkspaceReady(alphaProject, false)
	if err != nil {
		t.Fatalf("ensureWorkspaceReady(alpha) error: %v", err)
	}
	betaWorkspace, err := app.ensureWorkspaceReady(betaProject, false)
	if err != nil {
		t.Fatalf("ensureWorkspaceReady(beta) error: %v", err)
	}
	if _, err := app.store.SetWorkspaceSphere(betaWorkspace.ID, store.SphereWork); err != nil {
		t.Fatalf("SetWorkspaceSphere(beta) error: %v", err)
	}
	defaultProject, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspaceID(workspaceIDStr(defaultProject.ID)); err != nil {
		t.Fatalf("SetActiveWorkspaceID(default) error: %v", err)
	}
	if err := app.setActiveWorkspaceTracked(alphaWorkspace.ID, "workspace_switch"); err != nil {
		t.Fatalf("setActiveWorkspaceTracked(alpha) error: %v", err)
	}
	if err := app.setActiveWorkspaceTracked(betaWorkspace.ID, "workspace_switch"); err != nil {
		t.Fatalf("setActiveWorkspaceTracked(beta) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("projects list status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ActiveWorkspaceID != workspaceIDStr(betaProject.ID) {
		t.Fatalf("active_workspace_id = %q, want %q", payload.ActiveWorkspaceID, workspaceIDStr(betaProject.ID))
	}
}

func TestProjectsListMatchesStoredProjects(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	storedProjects, err := app.store.ListEnrichedWorkspaces()
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(payload.Projects) != len(storedProjects) {
		t.Fatalf("payload project count = %d, want %d", len(payload.Projects), len(storedProjects))
	}
	storedByID := make(map[string]store.Workspace, len(storedProjects))
	for _, project := range storedProjects {
		storedByID[workspaceIDStr(project.ID)] = project
	}
	for _, project := range payload.Projects {
		stored, ok := storedByID[project.ID]
		if !ok {
			t.Fatalf("unexpected project in payload: %#v", project)
		}
		if project.Name != stored.Name {
			t.Fatalf("project %q name = %q, want %q", project.ID, project.Name, stored.Name)
		}
	}
}

func TestProjectsListIncludesRunState(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	app.registerActiveChatTurn(session.ID, "run-projects", func() {})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 2
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range payload.Projects {
		if item.ID != workspaceIDStr(project.ID) {
			continue
		}
		if item.RunState.ActiveTurns != 1 {
			t.Fatalf("active_turns = %d, want 1", item.RunState.ActiveTurns)
		}
		if item.RunState.QueuedTurns != 2 {
			t.Fatalf("queued_turns = %d, want 2", item.RunState.QueuedTurns)
		}
		if !item.RunState.IsWorking {
			t.Fatalf("expected workspace to be working")
		}
		if item.RunState.Status != "running" {
			t.Fatalf("status = %q, want running", item.RunState.Status)
		}
		if item.RunState.ActiveTurnID != "run-projects" {
			t.Fatalf("active_turn_id = %q, want run-projects", item.RunState.ActiveTurnID)
		}
		return
	}
	t.Fatalf("expected workspace %d in list response", project.ID)
}
