package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

// TestItemProjectReviewListsActiveOutcomesWithHealth exercises every shape the
// composite-outcome review must report on: a project item with a next action
// (healthy), a waiting-only project item, a deferred-only project item, a
// someday-only project item, and a stalled project item with no actionable
// child. The endpoint must surface all five with correct health flags and
// must keep done outcomes out.
func TestItemProjectReviewListsActiveOutcomesWithHealth(t *testing.T) {
	app := newAuthedTestApp(t)

	specs := []struct {
		title       string
		childState  string
		role        string
		wantStalled bool
	}{
		{title: "Outcome with next action", childState: store.ItemStateNext, role: store.ItemLinkRoleNextAction},
		{title: "Outcome waiting only", childState: store.ItemStateWaiting, role: store.ItemLinkRoleSupport},
		{title: "Outcome deferred only", childState: store.ItemStateDeferred, role: store.ItemLinkRoleBlockedBy},
		{title: "Outcome someday only", childState: store.ItemStateSomeday, role: store.ItemLinkRoleSupport},
		{title: "Outcome stalled", childState: "", wantStalled: true},
	}
	for _, spec := range specs {
		parent, err := app.store.CreateItem(spec.title, store.ItemOptions{
			Kind:  store.ItemKindProject,
			State: store.ItemStateNext,
		})
		if err != nil {
			t.Fatalf("CreateItem(%q) error: %v", spec.title, err)
		}
		if spec.childState == "" {
			continue
		}
		child, err := app.store.CreateItem(spec.title+" child", store.ItemOptions{State: spec.childState})
		if err != nil {
			t.Fatalf("CreateItem(child %q) error: %v", spec.title, err)
		}
		if err := app.store.LinkItemChild(parent.ID, child.ID, spec.role); err != nil {
			t.Fatalf("LinkItemChild(%q) error: %v", spec.title, err)
		}
	}
	if _, err := app.store.CreateItem("Closed outcome", store.ItemOptions{
		Kind:  store.ItemKindProject,
		State: store.ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/projects", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	rows, ok := payload["project_items"].([]any)
	if !ok || len(rows) != len(specs) {
		t.Fatalf("project_items len = %d, want %d (done outcomes must not appear)", len(rows), len(specs))
	}
	if total, _ := payload["total"].(float64); int(total) != len(specs) {
		t.Fatalf("total = %v, want %d", payload["total"], len(specs))
	}
	stalledCount, _ := payload["stalled"].(float64)
	if int(stalledCount) != 1 {
		t.Fatalf("stalled = %v, want 1", payload["stalled"])
	}
	first := rows[0].(map[string]any)
	firstItem, _ := first["item"].(map[string]any)
	firstHealth, _ := first["health"].(map[string]any)
	if firstItem["title"] != "Outcome stalled" {
		t.Fatalf("first row title = %v, want %q (stalled outcomes must lead the weekly review)", firstItem["title"], "Outcome stalled")
	}
	if got, _ := firstHealth["stalled"].(bool); !got {
		t.Fatalf("first row stalled = %v, want true", firstHealth["stalled"])
	}

	healthByTitle := make(map[string]map[string]any, len(rows))
	for _, raw := range rows {
		row := raw.(map[string]any)
		item := row["item"].(map[string]any)
		if item["kind"] != store.ItemKindProject {
			t.Fatalf("review row kind = %v, want %q (only project items belong in the outcome review)", item["kind"], store.ItemKindProject)
		}
		if item["state"] == store.ItemStateDone {
			t.Fatalf("review row %q surfaced done outcome", item["title"])
		}
		healthByTitle[item["title"].(string)] = row["health"].(map[string]any)
	}
	healthExpect := map[string]struct {
		hasNext, hasWaiting, hasDeferred, hasSomeday, stalled bool
	}{
		"Outcome with next action": {hasNext: true},
		"Outcome waiting only":     {hasWaiting: true},
		"Outcome deferred only":    {hasDeferred: true},
		"Outcome someday only":     {hasSomeday: true},
		"Outcome stalled":          {stalled: true},
	}
	for title, expect := range healthExpect {
		got, ok := healthByTitle[title]
		if !ok {
			t.Fatalf("review missing %q", title)
		}
		if got["has_next_action"].(bool) != expect.hasNext {
			t.Fatalf("%q has_next_action = %v, want %v", title, got["has_next_action"], expect.hasNext)
		}
		if got["has_waiting"].(bool) != expect.hasWaiting {
			t.Fatalf("%q has_waiting = %v, want %v", title, got["has_waiting"], expect.hasWaiting)
		}
		if got["has_deferred"].(bool) != expect.hasDeferred {
			t.Fatalf("%q has_deferred = %v, want %v", title, got["has_deferred"], expect.hasDeferred)
		}
		if got["has_someday"].(bool) != expect.hasSomeday {
			t.Fatalf("%q has_someday = %v, want %v", title, got["has_someday"], expect.hasSomeday)
		}
		if got["stalled"].(bool) != expect.stalled {
			t.Fatalf("%q stalled = %v, want %v", title, got["stalled"], expect.stalled)
		}
	}
}

// TestItemProjectReviewWorkspaceFilterTreatsWorkspacesAsScopeNotOutcomes pins
// the issue's terminology contract: workspace_id narrows the scope of
// project-item review without ever turning a Workspace into a project item.
// A workspace with no project items must yield an empty review, even if it
// has plenty of regular action items.
func TestItemProjectReviewWorkspaceFilterTreatsWorkspacesAsScopeNotOutcomes(t *testing.T) {
	app := newAuthedTestApp(t)

	bare, err := app.store.CreateWorkspace("Bare workspace", t.TempDir(), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(bare) error: %v", err)
	}
	if _, err := app.store.CreateItem("Routine work", store.ItemOptions{
		Kind:        store.ItemKindAction,
		State:       store.ItemStateNext,
		WorkspaceID: &bare.ID,
	}); err != nil {
		t.Fatalf("CreateItem(routine) error: %v", err)
	}

	outcomeWorkspace, err := app.store.CreateWorkspace("Outcome workspace", t.TempDir(), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(outcome) error: %v", err)
	}
	if _, err := app.store.CreateItem("Linked outcome", store.ItemOptions{
		Kind:        store.ItemKindProject,
		State:       store.ItemStateNext,
		WorkspaceID: &outcomeWorkspace.ID,
	}); err != nil {
		t.Fatalf("CreateItem(linked outcome) error: %v", err)
	}

	bareReq := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/projects?workspace_id="+itoa(bare.ID), nil)
	if bareReq.Code != http.StatusOK {
		t.Fatalf("bare status = %d, want 200: %s", bareReq.Code, bareReq.Body.String())
	}
	barePayload := decodeJSONResponse(t, bareReq)
	rows, _ := barePayload["project_items"].([]any)
	if len(rows) != 0 {
		t.Fatalf("bare workspace review len = %d, want 0 (Workspaces never become outcomes)", len(rows))
	}

	scopedReq := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/projects?workspace_id="+itoa(outcomeWorkspace.ID), nil)
	if scopedReq.Code != http.StatusOK {
		t.Fatalf("outcome status = %d, want 200: %s", scopedReq.Code, scopedReq.Body.String())
	}
	scopedPayload := decodeJSONResponse(t, scopedReq)
	scopedRows, _ := scopedPayload["project_items"].([]any)
	if len(scopedRows) != 1 {
		t.Fatalf("outcome-workspace review len = %d, want 1", len(scopedRows))
	}
	scopedItem := scopedRows[0].(map[string]any)["item"].(map[string]any)
	if scopedItem["title"] != "Linked outcome" {
		t.Fatalf("outcome-workspace review surfaced %v, want %q", scopedItem["title"], "Linked outcome")
	}
	if scopedItem["kind"] != store.ItemKindProject {
		t.Fatalf("outcome-workspace review kind = %v, want %q", scopedItem["kind"], store.ItemKindProject)
	}
}

// TestItemProjectReviewSourceContainerStaysAFilter pins the second half of the
// terminology contract: a source container (Todoist project / GitHub Project)
// is only a metadata filter. It is never promoted into a project item, even
// when its source-backed actions are visible elsewhere.
func TestItemProjectReviewSourceContainerStaysAFilter(t *testing.T) {
	app := newAuthedTestApp(t)

	containerSource := store.ExternalProviderTodoist
	containerRef := "todoist-task-1"
	if _, err := app.store.CreateItem("Todoist next action", store.ItemOptions{
		Kind:      store.ItemKindAction,
		State:     store.ItemStateNext,
		Source:    &containerSource,
		SourceRef: &containerRef,
	}); err != nil {
		t.Fatalf("CreateItem(todoist action) error: %v", err)
	}

	project, err := app.store.CreateItem("Brain outcome", store.ItemOptions{
		Kind:  store.ItemKindProject,
		State: store.ItemStateNext,
	})
	if err != nil {
		t.Fatalf("CreateItem(project) error: %v", err)
	}
	if project.Source != nil || project.SourceRef != nil {
		t.Fatalf("brain-only outcome unexpectedly source-backed: %+v", project)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/projects", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	rows, _ := decodeJSONResponse(t, rr)["project_items"].([]any)
	if len(rows) != 1 {
		t.Fatalf("review len = %d, want 1 (source-container actions must not surface as outcomes)", len(rows))
	}
	if title := rows[0].(map[string]any)["item"].(map[string]any)["title"]; title != "Brain outcome" {
		t.Fatalf("review surfaced %v, want %q", title, "Brain outcome")
	}

	bodySnippet := strings.ToLower(rr.Body.String())
	for _, banned := range []string{"workspace", "source container"} {
		if strings.Contains(bodySnippet, banned) {
			t.Fatalf("response body unexpectedly contains %q: %s", banned, rr.Body.String())
		}
	}
}
