package web

import (
	"net/http"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

// The compact sidebar (issue #746) renders Workspace pin, item queues, and a
// secondary expandable section that surfaces project-item and recent-meeting
// counts as filters. The counts API must include both the per-state map and a
// `sections` payload so the frontend can render those filters without
// confusing project items with Workspaces.
func TestItemCountsExposesSidebarSectionCountsAlongsidePerStateCounts(t *testing.T) {
	app := newAuthedTestApp(t)

	if _, err := app.store.CreateItem("Plan GTD outcome", store.ItemOptions{
		Kind:  store.ItemKindProject,
		State: store.ItemStateNext,
	}); err != nil {
		t.Fatalf("CreateItem(project next) error: %v", err)
	}
	if _, err := app.store.CreateItem("Closed outcome", store.ItemOptions{
		Kind:  store.ItemKindProject,
		State: store.ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(project done) error: %v", err)
	}
	if _, err := app.store.CreateItem("Routine action", store.ItemOptions{
		State: store.ItemStateNext,
	}); err != nil {
		t.Fatalf("CreateItem(action) error: %v", err)
	}

	transcriptPath := "/tmp/transcript.md"
	transcriptTitle := "Recent transcript"
	if _, err := app.store.CreateArtifact(store.ArtifactKindTranscript, &transcriptPath, nil, &transcriptTitle, nil); err != nil {
		t.Fatalf("CreateArtifact(transcript) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/counts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("counts status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	counts, ok := payload["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts payload = %#v", payload)
	}
	if got := int(counts[store.ItemStateNext].(float64)); got != 2 {
		t.Fatalf("counts[next] = %d, want 2", got)
	}
	if got := int(counts[store.ItemStateDone].(float64)); got != 1 {
		t.Fatalf("counts[done] = %d, want 1", got)
	}

	sections, ok := payload["sections"].(map[string]any)
	if !ok {
		t.Fatalf("sections payload missing in %#v", payload)
	}
	if got := int(sections["project_items_open"].(float64)); got != 1 {
		t.Fatalf("sections[project_items_open] = %d, want 1 (only the open project item; done excluded)", got)
	}
	if got := int(sections["recent_meetings"].(float64)); got != 1 {
		t.Fatalf("sections[recent_meetings] = %d, want 1", got)
	}
}
