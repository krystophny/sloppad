package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestTemporaryProjectCreationCopiesSourceSettingsAndPersist(t *testing.T) {
	app := newAuthedTestApp(t)
	source, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceChatModel(workspaceIDStr(source.ID), "gpt"); err != nil {
		t.Fatalf("UpdateProjectChatModel() error: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceChatModelReasoningEffort(workspaceIDStr(source.ID), "xhigh"); err != nil {
		t.Fatalf("UpdateProjectChatModelReasoningEffort() error: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceCompanionConfig(workspaceIDStr(source.ID), `{"companion_enabled":true,"idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"kind":                "meeting",
		"source_workspace_id": workspaceIDStr(source.ID),
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		OK        bool `json:"ok"`
		Workspace struct {
			ID        string `json:"id"`
			Kind      string `json:"kind"`
			RootPath  string `json:"root_path"`
			ChatModel string `json:"chat_model"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createPayload.OK || createPayload.Workspace.ID == "" {
		t.Fatalf("expected created workspace payload")
	}
	if createPayload.Workspace.Kind != "meeting" {
		t.Fatalf("created kind = %q, want meeting", createPayload.Workspace.Kind)
	}
	if createPayload.Workspace.ChatModel != "gpt" {
		t.Fatalf("created chat model = %q, want gpt", createPayload.Workspace.ChatModel)
	}
	if createPayload.Workspace.RootPath == source.RootPath {
		t.Fatalf("temporary workspace root should differ from source root")
	}
	if !strings.Contains(filepath.ToSlash(createPayload.Workspace.RootPath), "/projects/temporary/meeting/") {
		t.Fatalf("temporary root = %q, want temporary meeting path", createPayload.Workspace.RootPath)
	}

	created, err := app.store.GetEnrichedWorkspace(createPayload.Workspace.ID)
	if err != nil {
		t.Fatalf("GetProject(created) error: %v", err)
	}
	if created.ChatModel != "gpt" {
		t.Fatalf("stored chat model = %q, want gpt", created.ChatModel)
	}
	if created.ChatModelReasoningEffort != "xhigh" {
		t.Fatalf("stored reasoning effort = %q, want xhigh", created.ChatModelReasoningEffort)
	}
	if got := strings.TrimSpace(created.CompanionConfigJSON); got != `{"companion_enabled":true,"idle_surface":"black"}` {
		t.Fatalf("stored companion config = %q", got)
	}
	workspace, err := app.ensureWorkspaceReady(created, false)
	if err != nil {
		t.Fatalf("ensureWorkspaceReady(created) error: %v", err)
	}
	artifactPath := filepath.Join(created.RootPath, "meeting-notes.md")
	if err := os.WriteFile(artifactPath, []byte("notes"), 0o644); err != nil {
		t.Fatalf("WriteFile(artifactPath) error: %v", err)
	}
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &artifactPath, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	participantSession, err := app.store.AddParticipantSession(created.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession() error: %v", err)
	}
	targetPath := filepath.Join(t.TempDir(), "persisted-meeting")

	rrPersist := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+createPayload.Workspace.ID+"/persist",
		map[string]any{
			"name": "Focused Meeting",
			"path": targetPath,
		},
	)
	if rrPersist.Code != http.StatusOK {
		t.Fatalf("expected persist 200, got %d: %s", rrPersist.Code, rrPersist.Body.String())
	}
	var persistPayload struct {
		OK        bool `json:"ok"`
		Workspace struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrPersist.Body.Bytes(), &persistPayload); err != nil {
		t.Fatalf("decode persist response: %v", err)
	}
	if !persistPayload.OK {
		t.Fatalf("expected persist ok=true")
	}
	if persistPayload.Workspace.Kind != "managed" {
		t.Fatalf("persisted kind = %q, want managed", persistPayload.Workspace.Kind)
	}
	persisted, err := app.store.GetEnrichedWorkspace(createPayload.Workspace.ID)
	if err != nil {
		t.Fatalf("GetProject(persisted) error: %v", err)
	}
	if persisted.Name != "Focused Meeting" {
		t.Fatalf("persisted name = %q, want Focused Meeting", persisted.Name)
	}
	if persisted.RootPath != targetPath {
		t.Fatalf("persisted root_path = %q, want %q", persisted.RootPath, targetPath)
	}
	updatedWorkspace, err := app.store.GetWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(updated) error: %v", err)
	}
	if updatedWorkspace.Name != "Focused Meeting" {
		t.Fatalf("workspace name = %q, want Focused Meeting", updatedWorkspace.Name)
	}
	if updatedWorkspace.DirPath != targetPath {
		t.Fatalf("workspace dir_path = %q, want %q", updatedWorkspace.DirPath, targetPath)
	}
	updatedArtifact, err := app.store.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if updatedArtifact.RefPath == nil || *updatedArtifact.RefPath != filepath.Join(targetPath, "meeting-notes.md") {
		t.Fatalf("artifact ref_path = %v, want moved path", updatedArtifact.RefPath)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "meeting-notes.md")); err != nil {
		t.Fatalf("Stat(moved artifact) error: %v", err)
	}
	updatedParticipantSession, err := app.store.GetParticipantSession(participantSession.ID)
	if err != nil {
		t.Fatalf("GetParticipantSession(updated) error: %v", err)
	}
	if updatedParticipantSession.WorkspacePath != targetPath {
		t.Fatalf("participant session workspace_path = %q, want %q", updatedParticipantSession.WorkspacePath, targetPath)
	}
}

func TestTemporaryProjectDiscardRemovesProjectDataAndFallsBackToDefaultProject(t *testing.T) {
	app := newAuthedTestApp(t)
	defaultProject, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces", map[string]any{
		"kind": "task",
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		Workspace struct {
			ID            string `json:"id"`
			WorkspacePath string `json:"workspace_path"`
			RootPath      string `json:"root_path"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Workspace.ID == "" {
		t.Fatalf("expected created task project id")
	}
	if err := os.WriteFile(filepath.Join(createPayload.Workspace.RootPath, "run-output.md"), []byte("saved output"), 0o644); err != nil {
		t.Fatalf("WriteFile(run-output.md) error: %v", err)
	}
	chatSession, err := app.store.GetOrCreateChatSession(createPayload.Workspace.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	workspace, err := app.store.GetWorkspace(chatSession.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace(chat workspace) error: %v", err)
	}
	if _, err := app.store.AddChatMessage(chatSession.ID, "assistant", "saved output", "saved output", "markdown"); err != nil {
		t.Fatalf("AddChatMessage() error: %v", err)
	}
	item, err := app.store.CreateItem("Temporary follow-up", store.ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	participantSession, err := app.store.AddParticipantSession(createPayload.Workspace.WorkspacePath, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession() error: %v", err)
	}
	if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: participantSession.ID,
		StartTS:   100,
		EndTS:     101,
		Text:      "transcript only",
		Status:    "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment() error: %v", err)
	}
	if err := app.store.UpsertParticipantRoomState(participantSession.ID, "summary", `["Acme"]`, `["Decision"]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	rrDiscard := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+createPayload.Workspace.ID+"/discard",
		map[string]any{},
	)
	if rrDiscard.Code != http.StatusOK {
		t.Fatalf("expected discard 200, got %d: %s", rrDiscard.Code, rrDiscard.Body.String())
	}
	var discardPayload struct {
		OK                bool   `json:"ok"`
		ActiveWorkspaceID string `json:"active_workspace_id"`
		ActiveWorkspace   struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"active_workspace"`
	}
	if err := json.Unmarshal(rrDiscard.Body.Bytes(), &discardPayload); err != nil {
		t.Fatalf("decode discard response: %v", err)
	}
	if !discardPayload.OK {
		t.Fatalf("expected discard ok=true")
	}
	if discardPayload.ActiveWorkspaceID != workspaceIDStr(defaultProject.ID) {
		t.Fatalf("active_workspace_id = %q, want %q", discardPayload.ActiveWorkspaceID, workspaceIDStr(defaultProject.ID))
	}
	if discardPayload.ActiveWorkspace.Kind != defaultProject.Kind {
		t.Fatalf("active project kind = %q, want %q", discardPayload.ActiveWorkspace.Kind, defaultProject.Kind)
	}
	if _, err := app.store.GetEnrichedWorkspace(createPayload.Workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProject(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := app.store.GetChatSession(chatSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatSession(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := app.store.GetWorkspaceByPath(createPayload.Workspace.RootPath); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWorkspaceByPath(discarded root) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := app.store.GetParticipantSession(participantSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetParticipantSession(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := os.Stat(createPayload.Workspace.RootPath); !os.IsNotExist(err) {
		t.Fatalf("temporary workspace root still exists: %v", err)
	}
	survivingItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(surviving) error: %v", err)
	}
	if survivingItem.WorkspaceID != nil {
		t.Fatalf("surviving item workspace_id = %v, want nil", survivingItem.WorkspaceID)
	}
	if survivingItem.WorkspaceID != nil {
		t.Fatalf("surviving item workspace_id = %v, want nil", survivingItem.WorkspaceID)
	}
	if survivingItem.Sphere != store.SpherePrivate {
		t.Fatalf("surviving item sphere = %q, want %q", survivingItem.Sphere, store.SpherePrivate)
	}
}
