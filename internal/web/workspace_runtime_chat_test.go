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

func TestCreateActivateProjectAffectsChatSessionCreation(t *testing.T) {
	app := newAuthedTestApp(t)

	linkedDir := t.TempDir()
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"name":     "linked-repo",
		"kind":     "linked",
		"path":     filepath.Clean(linkedDir),
		"activate": false,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		OK        bool `json:"ok"`
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createPayload.OK || createPayload.Workspace.ID == "" {
		t.Fatalf("expected created workspace id")
	}

	rrActivate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+createPayload.Workspace.ID+"/activate",
		map[string]any{},
	)
	if rrActivate.Code != http.StatusOK {
		t.Fatalf("expected activate 200, got %d: %s", rrActivate.Code, rrActivate.Body.String())
	}
	var activatePayload struct {
		OK                bool   `json:"ok"`
		ActiveWorkspaceID string `json:"active_workspace_id"`
		Project           struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrActivate.Body.Bytes(), &activatePayload); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if !activatePayload.OK {
		t.Fatalf("expected activate ok=true")
	}
	if activatePayload.ActiveWorkspaceID != createPayload.Workspace.ID {
		t.Fatalf("expected active project %q, got %q", createPayload.Workspace.ID, activatePayload.ActiveWorkspaceID)
	}

	rrSession := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions", map[string]any{})
	if rrSession.Code != http.StatusOK {
		t.Fatalf("expected chat session create 200, got %d: %s", rrSession.Code, rrSession.Body.String())
	}
	var sessionPayload struct {
		OK          bool   `json:"ok"`
		SessionID   string `json:"session_id"`
		WorkspaceID int64  `json:"workspace_id"`
	}
	if err := json.Unmarshal(rrSession.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatalf("decode chat session response: %v", err)
	}
	if !sessionPayload.OK {
		t.Fatalf("expected chat session create ok=true")
	}
	expectedWorkspaceID := runtimeWorkspaceIDInt64FromString(t, createPayload.Workspace.ID)
	if sessionPayload.WorkspaceID != expectedWorkspaceID {
		t.Fatalf("expected chat session workspace %d, got %d", expectedWorkspaceID, sessionPayload.WorkspaceID)
	}
	if sessionPayload.WorkspaceID <= 0 {
		t.Fatalf("expected workspace-backed chat session, got workspace_id=%d", sessionPayload.WorkspaceID)
	}
	session, err := app.store.GetChatSession(sessionPayload.SessionID)
	if err != nil {
		t.Fatalf("GetChatSession() error: %v", err)
	}
	if session.WorkspaceID != sessionPayload.WorkspaceID {
		t.Fatalf("session workspace_id = %d, want %d", session.WorkspaceID, sessionPayload.WorkspaceID)
	}
}

func TestCreateChatSessionWithoutSelectionStaysOnActiveWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.store.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() error: %v", err)
	}
	_ = anchor

	linkedDir := filepath.Join(t.TempDir(), "linked-repo")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(linkedDir) error: %v", err)
	}
	project, err := app.store.CreateEnrichedWorkspace("linked-repo", "linked-repo", linkedDir, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := app.store.SetActiveWorkspaceID(workspaceIDStr(project.ID)); err != nil {
		t.Fatalf("SetActiveWorkspaceID() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("chat session create status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		OK          bool   `json:"ok"`
		SessionID   string `json:"session_id"`
		WorkspaceID int64  `json:"workspace_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.WorkspaceID != anchor.ID {
		t.Fatalf("workspace_id = %d, want anchor %d", payload.WorkspaceID, anchor.ID)
	}
}

func TestProjectsListRehomesActiveProjectIntoActiveSphere(t *testing.T) {
	app := newAuthedTestApp(t)

	privateRoot := filepath.Join(t.TempDir(), "private-root")
	workRoot := filepath.Join(t.TempDir(), "work-root")
	for _, dir := range []string{privateRoot, workRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", dir, err)
		}
	}
	if _, err := app.store.CreateWorkspace("Private Root", privateRoot, store.SpherePrivate); err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	if _, err := app.store.CreateWorkspace("Work Root", workRoot, store.SphereWork); err != nil {
		t.Fatalf("CreateWorkspace(work) error: %v", err)
	}
	privateProjectPath := filepath.Join(privateRoot, "notes")
	workProjectPath := filepath.Join(workRoot, "tracker")
	for _, dir := range []string{privateProjectPath, workProjectPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", dir, err)
		}
	}
	_, err := app.store.CreateEnrichedWorkspace("Private Notes", "private-notes", privateProjectPath, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(private) error: %v", err)
	}
	workProject, err := app.store.CreateEnrichedWorkspace("Work Tracker", "work-tracker", workProjectPath, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(work) error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workProject.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(work) error: %v", err)
	}
	if err := app.store.SetActiveSphere(store.SpherePrivate); err != nil {
		t.Fatalf("SetActiveSphere(private) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("list projects status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload workspacesListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ActiveWorkspaceID == "" {
		t.Fatal("active_workspace_id is empty")
	}
}

func TestProjectActivateUpdatesActiveSphere(t *testing.T) {
	app := newAuthedTestApp(t)

	workRoot := filepath.Join(t.TempDir(), "work-root")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workRoot) error: %v", err)
	}
	if _, err := app.store.CreateWorkspace("Work Root", workRoot, store.SphereWork); err != nil {
		t.Fatalf("CreateWorkspace(work) error: %v", err)
	}
	projectPath := filepath.Join(workRoot, "tracker")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectPath) error: %v", err)
	}
	project, err := app.store.CreateEnrichedWorkspace("Work Tracker", "work-tracker", projectPath, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(work) error: %v", err)
	}
	if err := app.store.SetActiveSphere(store.SpherePrivate); err != nil {
		t.Fatalf("SetActiveSphere(private) error: %v", err)
	}

	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceIDStr(project.ID)+"/activate",
		map[string]any{},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("activate status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		OK                bool   `json:"ok"`
		ActiveWorkspaceID string `json:"active_workspace_id"`
		ActiveSphere      string `json:"active_sphere"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.ActiveWorkspaceID != workspaceIDStr(project.ID) {
		t.Fatalf("active_workspace_id = %q, want %q", payload.ActiveWorkspaceID, project.ID)
	}
	if payload.ActiveSphere != store.SphereWork {
		t.Fatalf("active_sphere = %q, want %q", payload.ActiveSphere, store.SphereWork)
	}
	activeSphere, err := app.store.ActiveSphere()
	if err != nil {
		t.Fatalf("ActiveSphere() error: %v", err)
	}
	if activeSphere != store.SphereWork {
		t.Fatalf("stored active sphere = %q, want %q", activeSphere, store.SphereWork)
	}
}

func TestWorkspaceSnapshotRejectsWorkPersonalActivation(t *testing.T) {
	_, personalRoot := configureWorkPersonalGuardrail(t)
	app := newAuthedTestApp(t)
	project, err := app.store.CreateEnrichedWorkspace("Personal", "personal", personalRoot, "linked", "", "", false)
	if err != nil {
		t.Fatalf("CreateEnrichedWorkspace(personal) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/"+workspaceIDStr(project.ID)+"/snapshot", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("snapshot status = %d, want 403: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "work personal subtree is blocked") {
		t.Fatalf("snapshot response = %q", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), personalRoot) {
		t.Fatalf("snapshot response leaked protected path: %q", rr.Body.String())
	}
}

func TestProjectChatModelUpdate(t *testing.T) {
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
	if workspaceID == "" {
		t.Fatalf("expected project id")
	}

	rrUpdate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceID+"/chat-model",
		map[string]any{"model": "gpt"},
	)
	if rrUpdate.Code != http.StatusBadRequest {
		t.Fatalf("expected update 400, got %d: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	if !strings.Contains(rrUpdate.Body.String(), "local is the only default dialogue model") {
		t.Fatalf("expected local-only error, got %q", rrUpdate.Body.String())
	}

	rrLocal := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceID+"/chat-model",
		map[string]any{"model": "local"},
	)
	if rrLocal.Code != http.StatusOK {
		t.Fatalf("expected local update 200, got %d: %s", rrLocal.Code, rrLocal.Body.String())
	}
	var updatePayload struct {
		OK        bool `json:"ok"`
		Workspace struct {
			ID                       string `json:"id"`
			ChatModel                string `json:"chat_model"`
			ChatModelReasoningEffort string `json:"chat_model_reasoning_effort"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrLocal.Body.Bytes(), &updatePayload); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if !updatePayload.OK {
		t.Fatalf("expected update ok=true")
	}
	if updatePayload.Workspace.ID != workspaceID {
		t.Fatalf("expected updated workspace id %q, got %q", workspaceID, updatePayload.Workspace.ID)
	}
	if updatePayload.Workspace.ChatModel != "local" {
		t.Fatalf("expected chat model local, got %q", updatePayload.Workspace.ChatModel)
	}
	if updatePayload.Workspace.ChatModelReasoningEffort != "none" {
		t.Fatalf("expected local reasoning effort none, got %q", updatePayload.Workspace.ChatModelReasoningEffort)
	}

	rrEffortUpdate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceID+"/chat-model",
		map[string]any{
			"model":            "local",
			"reasoning_effort": "extra_high",
		},
	)
	if rrEffortUpdate.Code != http.StatusOK {
		t.Fatalf("expected effort update 200, got %d: %s", rrEffortUpdate.Code, rrEffortUpdate.Body.String())
	}
	var effortPayload struct {
		OK        bool `json:"ok"`
		Workspace struct {
			ID                       string `json:"id"`
			ChatModelReasoningEffort string `json:"chat_model_reasoning_effort"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrEffortUpdate.Body.Bytes(), &effortPayload); err != nil {
		t.Fatalf("decode effort update response: %v", err)
	}
	if !effortPayload.OK {
		t.Fatalf("expected effort update ok=true")
	}
	if effortPayload.Workspace.ChatModelReasoningEffort != "none" {
		t.Fatalf("expected effort none, got %q", effortPayload.Workspace.ChatModelReasoningEffort)
	}

	rrInvalid := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceID+"/chat-model",
		map[string]any{"model": "invalid"},
	)
	if rrInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid model 400, got %d: %s", rrInvalid.Code, rrInvalid.Body.String())
	}
}

func TestProjectChatModelUpdateAllowsDefaultProject(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}

	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceIDStr(project.ID)+"/chat-model",
		map[string]any{"model": "gpt"},
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "local is the only default dialogue model") {
		t.Fatalf("expected local-only error, got %q", rr.Body.String())
	}
}

func TestProjectsActivityListsPerProjectRunState(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/activity", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected activity 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload workspacesActivityResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode activity response: %v", err)
	}
	for _, item := range payload.Projects {
		if item.WorkspaceID != workspaceIDStr(project.ID) {
			continue
		}
		if item.ChatSessionID != session.ID {
			t.Fatalf("chat_session_id = %q, want %q", item.ChatSessionID, session.ID)
		}
		if item.RunState.ActiveTurns != 0 {
			t.Fatalf("active_turns = %d, want 0", item.RunState.ActiveTurns)
		}
		if item.RunState.QueuedTurns != 3 {
			t.Fatalf("queued_turns = %d, want 3", item.RunState.QueuedTurns)
		}
		if !item.RunState.IsWorking {
			t.Fatalf("expected workspace to be working")
		}
		if item.RunState.Status != "queued" {
			t.Fatalf("status = %q, want queued", item.RunState.Status)
		}
		return
	}
	t.Fatalf("expected workspace %d in activity response", project.ID)
}

func TestProjectsActivityUnreadClearsOnActivate(t *testing.T) {
	app := newAuthedTestApp(t)

	linkedDir := t.TempDir()
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"name":     "Unread Test",
		"kind":     "linked",
		"path":     filepath.Clean(linkedDir),
		"activate": false,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	project, err := app.store.GetEnrichedWorkspace(createPayload.Workspace.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	app.markWorkspaceOutput(project.WorkspacePath)

	findActivity := func() workspacesActivityResponse {
		t.Helper()
		rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/activity", map[string]any{})
		if rr.Code != http.StatusOK {
			t.Fatalf("expected activity 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var payload workspacesActivityResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode activity response: %v", err)
		}
		return payload
	}

	initial := findActivity()
	foundUnread := false
	for _, item := range initial.Projects {
		if item.WorkspaceID != workspaceIDStr(project.ID) {
			continue
		}
		foundUnread = true
		if !item.Unread {
			t.Fatalf("expected unread=true before activation")
		}
		if item.ReviewPending {
			t.Fatalf("expected review_pending=false before activation")
		}
	}
	if !foundUnread {
		t.Fatalf("expected project %q in activity response", project.ID)
	}

	rrActivate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+workspaceIDStr(project.ID)+"/activate",
		map[string]any{},
	)
	if rrActivate.Code != http.StatusOK {
		t.Fatalf("expected activate 200, got %d: %s", rrActivate.Code, rrActivate.Body.String())
	}

	afterActivate := findActivity()
	for _, item := range afterActivate.Projects {
		if item.WorkspaceID != workspaceIDStr(project.ID) {
			continue
		}
		if item.Unread {
			t.Fatalf("expected unread=false after activation")
		}
		if item.ReviewPending {
			t.Fatalf("expected review_pending=false after activation")
		}
		return
	}
	t.Fatalf("expected workspace %d in activity response after activation", project.ID)
}
