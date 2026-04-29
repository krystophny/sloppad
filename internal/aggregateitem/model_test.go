package aggregateitem

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestAggregateItemSupportsAcceptedSourceKinds(t *testing.T) {
	now := mustTime(t, "2026-04-29T09:30:00Z")
	accountID := int64(42)
	container := "inbox"
	url := "https://example.test/item"

	bindings := []SourceBinding{
		{Kind: SourceKindMarkdown, SourceRef: "notes/actions.md#L12"},
		{Kind: SourceKindTodoist, Provider: "todoist", AccountID: &accountID, ObjectType: "task", RemoteID: "task-1", ContainerRef: &container},
		{Kind: SourceKindGitHub, Provider: "github", ObjectType: "issue", RemoteID: "sloppy-org/slopshell#725", URL: &url},
		{Kind: SourceKindGitLab, Provider: "gitlab", ObjectType: "issue", RemoteID: "plasma/slopshell#11", RemoteUpdatedAt: &now},
		{Kind: SourceKindEmail, Provider: "exchange_ews", AccountID: &accountID, ObjectType: "email", RemoteID: "AAMk-msg"},
		{Kind: SourceKindLocal, SourceRef: "item:17"},
	}

	item, err := New("aggregate-1", bindings, SourceFields{Title: "Follow up", State: "open"}, LocalOverlay{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	wantKinds := []SourceKind{
		SourceKindMarkdown,
		SourceKindTodoist,
		SourceKindGitHub,
		SourceKindGitLab,
		SourceKindEmail,
		SourceKindLocal,
	}
	if !reflect.DeepEqual(item.Projection.SourceKinds, wantKinds) {
		t.Fatalf("Projection.SourceKinds = %#v, want %#v", item.Projection.SourceKinds, wantKinds)
	}
	if got := item.Bindings[1].Authority.Backend; got != string(SourceKindTodoist) {
		t.Fatalf("todoist authority backend = %q, want todoist", got)
	}
	if got := item.Bindings[4].Provider; got != "exchange_ews" {
		t.Fatalf("email provider = %q, want exchange_ews", got)
	}
}

func TestAggregateItemProjectionSeparatesSourceFieldsAndOverlay(t *testing.T) {
	workspaceID := int64(7)
	artifactID := int64(9)
	sphere := "work"
	overlayState := "someday"
	overlayTitle := "Local title"
	dueAt := mustTime(t, "2026-05-04T10:00:00Z")
	visibleAfter := mustTime(t, "2026-05-01T08:00:00Z")

	item, err := New("todoist:task-1", []SourceBinding{{
		Kind:       SourceKindTodoist,
		Provider:   "Todoist",
		ObjectType: "Task",
		RemoteID:   "task-1",
		Authority: BackendAuthority{
			Backend:            "todoist",
			SourceFields:       []string{"title", "state", "due_at", "labels"},
			LocalOverlayFields: []string{"workspace_id", "sphere", "visible_after"},
		},
	}}, SourceFields{
		Title:  "Remote title",
		State:  "open",
		DueAt:  &dueAt,
		Labels: []string{"Remote", "Project"},
	}, LocalOverlay{
		Title:        &overlayTitle,
		State:        &overlayState,
		WorkspaceID:  &workspaceID,
		Sphere:       &sphere,
		ArtifactID:   &artifactID,
		VisibleAfter: &visibleAfter,
		Labels:       []string{"Local", "project"},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if item.Source.Title != "Remote title" || item.Overlay.Title == nil || *item.Overlay.Title != "Local title" {
		t.Fatalf("source/overlay title separation failed: %#v %#v", item.Source, item.Overlay)
	}
	if item.Projection.Title != "Remote title" {
		t.Fatalf("Projection.Title = %q, want source-owned title", item.Projection.Title)
	}
	if item.Projection.State != StateInbox {
		t.Fatalf("Projection.State = %q, want inbox from source state", item.Projection.State)
	}
	if item.Projection.WorkspaceID == nil || *item.Projection.WorkspaceID != workspaceID {
		t.Fatalf("Projection.WorkspaceID = %v, want %d", item.Projection.WorkspaceID, workspaceID)
	}
	wantLabels := []string{"remote", "project", "local"}
	if !reflect.DeepEqual(item.Projection.Labels, wantLabels) {
		t.Fatalf("Projection.Labels = %#v, want %#v", item.Projection.Labels, wantLabels)
	}
	if item.Projection.VisibleAfter == nil || !item.Projection.VisibleAfter.Equal(visibleAfter) {
		t.Fatalf("Projection.VisibleAfter = %v, want %v", item.Projection.VisibleAfter, visibleAfter)
	}
}

func TestAggregateItemUsesOverlayWhenLocalOwnsField(t *testing.T) {
	title := "Local display title"
	state := "waiting"

	item, err := New("local:17", []SourceBinding{{
		Kind:      SourceKindLocal,
		SourceRef: "item:17",
		Authority: BackendAuthority{
			Backend:            "local",
			LocalOverlayFields: []string{"title", "state"},
		},
	}}, SourceFields{
		Title: "Stored title",
		State: "open",
	}, LocalOverlay{
		Title: &title,
		State: &state,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if item.Projection.Title != title {
		t.Fatalf("Projection.Title = %q, want %q", item.Projection.Title, title)
	}
	if item.Projection.State != StateWaiting {
		t.Fatalf("Projection.State = %q, want %q", item.Projection.State, StateWaiting)
	}
}

func TestAggregateItemJSONRoundTripPreservesBoundary(t *testing.T) {
	followUpAt := mustTime(t, "2026-05-02T12:00:00Z")
	sphere := "private"

	item, err := New("github:sloppy-org/slopshell#725", []SourceBinding{{
		Kind:       SourceKindGitHub,
		Provider:   "github",
		ObjectType: "issue",
		RemoteID:   "sloppy-org/slopshell#725",
		Authority: BackendAuthority{
			Backend:            "github",
			SourceFields:       []string{"title", "state"},
			LocalOverlayFields: []string{"sphere", "follow_up_at"},
		},
	}}, SourceFields{
		Title: "Add aggregate item model",
		State: "open",
	}, LocalOverlay{
		Sphere:     &sphere,
		FollowUpAt: &followUpAt,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var got AggregateItem
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if got.Bindings[0].RemoteID != item.Bindings[0].RemoteID {
		t.Fatalf("round-trip binding remote id = %q, want %q", got.Bindings[0].RemoteID, item.Bindings[0].RemoteID)
	}
	if !reflect.DeepEqual(got.Bindings[0].Authority.SourceFields, []string{"state", "title"}) {
		t.Fatalf("round-trip source authority = %#v", got.Bindings[0].Authority.SourceFields)
	}
	if got.Source.Title != item.Source.Title {
		t.Fatalf("round-trip source title = %q, want %q", got.Source.Title, item.Source.Title)
	}
	if got.Overlay.Sphere == nil || *got.Overlay.Sphere != sphere {
		t.Fatalf("round-trip overlay sphere = %v, want %q", got.Overlay.Sphere, sphere)
	}
	if got.Projection.FollowUpAt == nil || !got.Projection.FollowUpAt.Equal(followUpAt) {
		t.Fatalf("round-trip projection follow_up_at = %v, want %v", got.Projection.FollowUpAt, followUpAt)
	}
}

func TestAggregateItemRejectsIncompleteRemoteBinding(t *testing.T) {
	_, err := New("bad", []SourceBinding{{
		Kind:     SourceKindGitLab,
		Provider: "gitlab",
		RemoteID: "plasma/project#1",
	}}, SourceFields{Title: "broken"}, LocalOverlay{})
	if err == nil {
		t.Fatal("New() error = nil, want missing object_type error")
	}
}

func TestFromStoreItemBuildsAggregateBoundary(t *testing.T) {
	workspaceID := int64(3)
	accountID := int64(5)
	source := store.ExternalProviderTodoist
	sourceRef := "task:task-1"
	followUpAt := "2026-05-02T12:00:00Z"
	item := store.Item{
		ID:          17,
		Title:       "Remote task",
		State:       store.ItemStateInbox,
		WorkspaceID: &workspaceID,
		Sphere:      store.SphereWork,
		FollowUpAt:  &followUpAt,
		Source:      &source,
		SourceRef:   &sourceRef,
	}

	got, err := FromStoreItem(item, []store.ExternalBinding{{
		AccountID:  accountID,
		Provider:   store.ExternalProviderTodoist,
		ObjectType: "task",
		RemoteID:   "task-1",
	}})
	if err != nil {
		t.Fatalf("FromStoreItem() error: %v", err)
	}

	if got.ID != "todoist:task:task-1" {
		t.Fatalf("aggregate ID = %q, want todoist source id", got.ID)
	}
	if got.Bindings[0].Kind != SourceKindTodoist || got.Bindings[0].AccountID == nil || *got.Bindings[0].AccountID != accountID {
		t.Fatalf("binding = %#v, want todoist account binding", got.Bindings[0])
	}
	if got.Source.Title != item.Title || got.Overlay.WorkspaceID == nil || *got.Overlay.WorkspaceID != workspaceID {
		t.Fatalf("source/overlay mapping failed: %#v %#v", got.Source, got.Overlay)
	}
	if got.Projection.FollowUpAt == nil || got.Projection.FollowUpAt.Format(time.RFC3339) != followUpAt {
		t.Fatalf("projection follow_up_at = %v, want %s", got.Projection.FollowUpAt, followUpAt)
	}
}

func mustTime(t *testing.T, raw string) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return value
}
