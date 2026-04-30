package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestItemWorkspaceReassignmentAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	oldWorkspace, err := app.store.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	newWorkspace, err := app.store.CreateWorkspace("Beta", filepath.Join(t.TempDir(), "beta"))
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	refPath := filepath.Join(oldWorkspace.DirPath, "README.md")
	title := "README.md"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &refPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review workspace assignment", store.ItemOptions{
		WorkspaceID: &oldWorkspace.ID,
		ArtifactID:  &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrWorkspace := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/workspace", map[string]any{
		"workspace_id": newWorkspace.ID,
	})
	if rrWorkspace.Code != http.StatusOK {
		t.Fatalf("workspace reassignment status = %d, want 200: %s", rrWorkspace.Code, rrWorkspace.Body.String())
	}
	workspacePayload := decodeJSONResponse(t, rrWorkspace)
	if got := strFromAny(workspacePayload["warning"]); got == "" {
		t.Fatalf("workspace warning = %q, want artifact link warning", got)
	}
	updatedItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(reassigned workspace) error: %v", err)
	}
	if updatedItem.WorkspaceID == nil || *updatedItem.WorkspaceID != newWorkspace.ID {
		t.Fatalf("workspace_id = %v, want %d", updatedItem.WorkspaceID, newWorkspace.ID)
	}
	updatedArtifact, err := app.store.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if updatedArtifact.RefPath == nil || *updatedArtifact.RefPath != refPath {
		t.Fatalf("artifact ref_path = %v, want %q", updatedArtifact.RefPath, refPath)
	}

	rrClearWorkspace := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/workspace", map[string]any{
		"workspace_id": nil,
	})
	if rrClearWorkspace.Code != http.StatusOK {
		t.Fatalf("clear workspace status = %d, want 200: %s", rrClearWorkspace.Code, rrClearWorkspace.Body.String())
	}
	updatedItem, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(cleared workspace) error: %v", err)
	}
	if updatedItem.WorkspaceID != nil {
		t.Fatalf("cleared workspace_id = %v, want nil", updatedItem.WorkspaceID)
	}
}

func TestLegacyItemProjectReassignmentRouteRemoved(t *testing.T) {
	app := newAuthedTestApp(t)
	item, err := app.store.CreateItem("Reassign me", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrProject := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/project", map[string]any{
		"workspace_id": "missing-project",
	})
	if rrProject.Code != http.StatusNotFound {
		t.Fatalf("legacy project route status = %d, want 404: %s", rrProject.Code, rrProject.Body.String())
	}
}

func TestItemWorkspaceReassignmentAPIRejectsUnknownWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	item, err := app.store.CreateItem("Reassign me", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrWorkspace := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/workspace", map[string]any{
		"workspace_id": 9999,
	})
	if rrWorkspace.Code != http.StatusBadRequest {
		t.Fatalf("unknown workspace status = %d, want 400: %s", rrWorkspace.Code, rrWorkspace.Body.String())
	}
}

func TestItemProjectItemLinkAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.store.CreateItem("Ship unified inbox", store.ItemOptions{
		Kind:  store.ItemKindProject,
		State: store.ItemStateNext,
	})
	if err != nil {
		t.Fatalf("CreateItem(project) error: %v", err)
	}
	item, err := app.store.CreateItem("Clarify imported capture", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(child) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(item.ID)+"/project-item-link", map[string]any{
		"project_item_id": project.ID,
		"role":            store.ItemLinkRoleNextAction,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("project item link status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	assertProjectItemLinkResponse(t, decodeJSONResponse(t, rr), project.ID, item.ID)

	rrSelf := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(item.ID)+"/project-item-link", map[string]any{
		"project_item_id": item.ID,
	})
	if rrSelf.Code != http.StatusBadRequest {
		t.Fatalf("self link status = %d, want 400: %s", rrSelf.Code, rrSelf.Body.String())
	}
}

func assertProjectItemLinkResponse(t *testing.T, payload map[string]any, projectID, itemID int64) {
	t.Helper()
	if got := int64(payload["project_item_id"].(float64)); got != projectID {
		t.Fatalf("project_item_id = %d, want %d", got, projectID)
	}
	links, ok := payload["links"].([]any)
	if !ok || len(links) != 1 {
		t.Fatalf("links payload = %#v, want one link", payload["links"])
	}
	first, ok := links[0].(map[string]any)
	if !ok {
		t.Fatalf("link payload = %#v", links[0])
	}
	if got := int64(first["child_item_id"].(float64)); got != itemID {
		t.Fatalf("child_item_id = %d, want %d", got, itemID)
	}
	if got := strFromAny(first["role"]); got != store.ItemLinkRoleNextAction {
		t.Fatalf("role = %q, want %q", got, store.ItemLinkRoleNextAction)
	}
	health, ok := payload["health"].(map[string]any)
	if !ok {
		t.Fatalf("health payload = %#v", payload["health"])
	}
	if stalled, _ := health["stalled"].(bool); !stalled {
		t.Fatalf("health = %#v, want stalled true for inbox-only linked item", health)
	}
}
