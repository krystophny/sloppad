package web

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestContextQueryFiltersWorkspaceItemAndArtifactAPIs(t *testing.T) {
	app := newAuthedTestApp(t)

	work, err := app.store.CreateLabel("Work", nil)
	if err != nil {
		t.Fatalf("CreateLabel(work) error: %v", err)
	}
	w7x, err := app.store.CreateLabel("W7x", &work.ID)
	if err != nil {
		t.Fatalf("CreateLabel(w7x) error: %v", err)
	}
	privateCtx, err := app.store.CreateLabel("Private", nil)
	if err != nil {
		t.Fatalf("CreateLabel(private) error: %v", err)
	}

	workspaceDir := filepath.Join(t.TempDir(), "w7x")
	workspace, err := app.store.CreateWorkspace("W7x Workspace", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.LinkLabelToWorkspace(w7x.ID, workspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace() error: %v", err)
	}
	privateWorkspace, err := app.store.CreateWorkspace("Private Workspace", filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	if err := app.store.LinkLabelToWorkspace(privateCtx.ID, privateWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(private) error: %v", err)
	}

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	workItem, err := app.store.CreateItem("Work inbox item", store.ItemOptions{
		State:        store.ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(work) error: %v", err)
	}
	privateItem, err := app.store.CreateItem("Private inbox item", store.ItemOptions{
		State:        store.ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(private) error: %v", err)
	}
	if err := app.store.LinkLabelToItem(privateCtx.ID, privateItem.ID); err != nil {
		t.Fatalf("LinkLabelToItem(private) error: %v", err)
	}

	workArtifactPath := filepath.Join(workspaceDir, "artifact.md")
	workArtifactTitle := "Work artifact"
	workArtifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &workArtifactPath, nil, &workArtifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(work) error: %v", err)
	}
	privateArtifactTitle := "Private artifact"
	privateArtifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, nil, nil, &privateArtifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(private) error: %v", err)
	}
	if err := app.store.LinkLabelToArtifact(privateCtx.ID, privateArtifact.ID); err != nil {
		t.Fatalf("LinkLabelToArtifact(private) error: %v", err)
	}

	rrWorkspaces := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces?context=work/w7x", nil)
	if rrWorkspaces.Code != http.StatusOK {
		t.Fatalf("workspace context status = %d, want 200: %s", rrWorkspaces.Code, rrWorkspaces.Body.String())
	}
	workspaces, ok := decodeJSONDataResponse(t, rrWorkspaces)["workspaces"].([]any)
	if !ok || len(workspaces) != 1 {
		t.Fatalf("workspace context payload = %#v", decodeJSONDataResponse(t, rrWorkspaces))
	}
	if got := int64(workspaces[0].(map[string]any)["id"].(float64)); got != workspace.ID {
		t.Fatalf("workspace context id = %d, want %d", got, workspace.ID)
	}

	rrItems := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?label=work", nil)
	if rrItems.Code != http.StatusOK {
		t.Fatalf("item label status = %d, want 200: %s", rrItems.Code, rrItems.Body.String())
	}
	items, ok := decodeJSONDataResponse(t, rrItems)["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("item label payload = %#v", decodeJSONDataResponse(t, rrItems))
	}
	if got := int64(items[0].(map[string]any)["id"].(float64)); got != workItem.ID {
		t.Fatalf("item label id = %d, want %d", got, workItem.ID)
	}

	rrArtifacts := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?context=w7x", nil)
	if rrArtifacts.Code != http.StatusOK {
		t.Fatalf("artifact context status = %d, want 200: %s", rrArtifacts.Code, rrArtifacts.Body.String())
	}
	artifacts, ok := decodeJSONDataResponse(t, rrArtifacts)["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("artifact context payload = %#v", decodeJSONDataResponse(t, rrArtifacts))
	}
	if got := int64(artifacts[0].(map[string]any)["id"].(float64)); got != workArtifact.ID {
		t.Fatalf("artifact context id = %d, want %d", got, workArtifact.ID)
	}
}

func TestItemLabelQueryRejectsLabelAndLabelIDTogether(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?label=work&label_id=1", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("conflicting label filters status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
}

func TestArtifactContextQueryCombinesDateAndTopicFilters(t *testing.T) {
	app := newAuthedTestApp(t)

	plasmaWorkspace, err := app.store.EnsureDailyWorkspace("2026-03-11", filepath.Join(t.TempDir(), "daily", "2026", "03", "11", "plasma"))
	if err != nil {
		t.Fatalf("EnsureDailyWorkspace(plasma) error: %v", err)
	}
	healthWorkspace, err := app.store.CreateWorkspace("Health notes", filepath.Join(t.TempDir(), "health"))
	if err != nil {
		t.Fatalf("CreateWorkspace(health) error: %v", err)
	}

	workRoot := labelByNameForTest(t, app, "work")
	privateRoot := labelByNameForTest(t, app, "private")
	plasmaContext, err := app.store.CreateLabel("work/plasma", &workRoot.ID)
	if err != nil {
		t.Fatalf("CreateLabel(work/plasma) error: %v", err)
	}
	healthContext, err := app.store.CreateLabel("private/health", &privateRoot.ID)
	if err != nil {
		t.Fatalf("CreateLabel(private/health) error: %v", err)
	}
	marchDay := labelByNameForTest(t, app, "2026/03/11")
	if err := app.store.LinkLabelToWorkspace(plasmaContext.ID, plasmaWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(plasma) error: %v", err)
	}
	if err := app.store.LinkLabelToWorkspace(marchDay.ID, healthWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(march day) error: %v", err)
	}
	if err := app.store.LinkLabelToWorkspace(healthContext.ID, healthWorkspace.ID); err != nil {
		t.Fatalf("LinkLabelToWorkspace(health) error: %v", err)
	}

	plasmaPath := filepath.Join(plasmaWorkspace.DirPath, "plan.md")
	plasmaTitle := "Plasma plan"
	plasmaArtifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &plasmaPath, nil, &plasmaTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(plasma) error: %v", err)
	}
	healthPath := filepath.Join(healthWorkspace.DirPath, "notes.md")
	healthTitle := "Health notes"
	if _, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &healthPath, nil, &healthTitle, nil); err != nil {
		t.Fatalf("CreateArtifact(health) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?context=2026/03/11%20%2B%20work/plasma", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("artifact combined context status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	artifacts, ok := decodeJSONDataResponse(t, rr)["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("artifact combined context payload = %#v", decodeJSONDataResponse(t, rr))
	}
	if got := int64(artifacts[0].(map[string]any)["id"].(float64)); got != plasmaArtifact.ID {
		t.Fatalf("artifact combined context id = %d, want %d", got, plasmaArtifact.ID)
	}
}

func labelByNameForTest(t *testing.T, app *App, name string) store.Label {
	t.Helper()
	labels, err := app.store.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels() error: %v", err)
	}
	for _, label := range labels {
		if label.Name == name {
			return label
		}
	}
	t.Fatalf("label %q not found", name)
	return store.Label{}
}
