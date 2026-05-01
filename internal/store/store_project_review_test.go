package store

import "testing"

type projectReviewSpec struct {
	title          string
	childState     string
	role           string
	wantNextAction bool
	wantWaiting    bool
	wantDeferred   bool
	wantSomeday    bool
	wantStalled    bool
	wantTotal      int
}

// TestListProjectItemReviewsCoversAllHealthStates exercises the five GTD
// project-item shapes the issue calls out: a project item with a next action,
// a waiting-only project item, a deferred-only project item, a someday-only
// project item, and a stalled project item with no actionable child. The
// resulting review surface must report each one's health correctly, must keep
// done project items off the list, and must surface stalled project items
// first so weekly review walks the riskiest outcomes before the rest.
func TestListProjectItemReviewsCoversAllHealthStates(t *testing.T) {
	s := newTestStore(t)

	// Closed outcome must be filtered out — done project items never appear in
	// review.
	if _, err := s.CreateItem("Already shipped", ItemOptions{
		Kind:  ItemKindProject,
		State: ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(done project) error: %v", err)
	}

	specs := projectReviewHealthSpecs()
	parents := seedProjectReviewSpecs(t, s, specs)

	// A done child must count toward Total but never restore health: it is
	// closed work, not an open loop. Add one to the stalled outcome to prove
	// the stalled flag survives done children.
	stalledID := parents["Outcome stalled"]
	doneChild, err := s.CreateItem("Stalled done child", ItemOptions{State: ItemStateDone})
	if err != nil {
		t.Fatalf("CreateItem(done child) error: %v", err)
	}
	if err := s.LinkItemChild(stalledID, doneChild.ID, ItemLinkRoleSupport); err != nil {
		t.Fatalf("LinkItemChild(done child) error: %v", err)
	}

	reviews, err := s.ListProjectItemReviewsFiltered(ItemListFilter{})
	if err != nil {
		t.Fatalf("ListProjectItemReviewsFiltered() error: %v", err)
	}
	if len(reviews) != len(specs) {
		t.Fatalf("review len = %d, want %d (done project items must not appear)", len(reviews), len(specs))
	}
	if !reviews[0].Health.Stalled {
		t.Fatalf("review[0] stalled = false, want true (stalled outcomes must lead the weekly review traversal)")
	}

	assertProjectReviewHealthSpecs(t, reviews, specs)
}

func projectReviewHealthSpecs() []projectReviewSpec {
	return []projectReviewSpec{
		{title: "Outcome with next action", childState: ItemStateNext, role: ItemLinkRoleNextAction, wantNextAction: true, wantTotal: 1},
		{title: "Outcome waiting only", childState: ItemStateWaiting, role: ItemLinkRoleSupport, wantWaiting: true, wantTotal: 1},
		{title: "Outcome deferred only", childState: ItemStateDeferred, role: ItemLinkRoleBlockedBy, wantDeferred: true, wantTotal: 1},
		{title: "Outcome someday only", childState: ItemStateSomeday, role: ItemLinkRoleSupport, wantSomeday: true, wantTotal: 1},
		{title: "Outcome stalled", wantStalled: true},
	}
}

func seedProjectReviewSpecs(t *testing.T, s *Store, specs []projectReviewSpec) map[string]int64 {
	t.Helper()
	parents := make(map[string]int64, len(specs))
	for _, spec := range specs {
		parent, err := s.CreateItem(spec.title, ItemOptions{
			Kind:  ItemKindProject,
			State: ItemStateNext,
		})
		if err != nil {
			t.Fatalf("CreateItem(%q) error: %v", spec.title, err)
		}
		parents[spec.title] = parent.ID
		if spec.childState == "" {
			continue
		}
		child, err := s.CreateItem(spec.title+" child", ItemOptions{State: spec.childState})
		if err != nil {
			t.Fatalf("CreateItem(%q child) error: %v", spec.title, err)
		}
		if err := s.LinkItemChild(parent.ID, child.ID, spec.role); err != nil {
			t.Fatalf("LinkItemChild(%q) error: %v", spec.title, err)
		}
	}
	return parents
}

func assertProjectReviewHealthSpecs(t *testing.T, reviews []ProjectItemReview, specs []projectReviewSpec) {
	t.Helper()
	byTitle := make(map[string]ProjectItemReview, len(reviews))
	for _, review := range reviews {
		if review.Item.Kind != ItemKindProject {
			t.Fatalf("review item Kind = %q, want %q (review surface must only contain project items)", review.Item.Kind, ItemKindProject)
		}
		if review.Item.State == ItemStateDone {
			t.Fatalf("review surfaced done outcome %q (must filter out done project items)", review.Item.Title)
		}
		byTitle[review.Item.Title] = review
	}
	for _, spec := range specs {
		assertProjectReviewHealthSpec(t, byTitle, spec)
	}
}

func assertProjectReviewHealthSpec(t *testing.T, reviews map[string]ProjectItemReview, spec projectReviewSpec) {
	t.Helper()
	got, ok := reviews[spec.title]
	if !ok {
		t.Fatalf("review missing %q; titles=%v", spec.title, mapKeys(reviews))
	}
	if got.Health.HasNextAction != spec.wantNextAction {
		t.Fatalf("%q HasNextAction = %t, want %t", spec.title, got.Health.HasNextAction, spec.wantNextAction)
	}
	if got.Health.HasWaiting != spec.wantWaiting {
		t.Fatalf("%q HasWaiting = %t, want %t", spec.title, got.Health.HasWaiting, spec.wantWaiting)
	}
	if got.Health.HasDeferred != spec.wantDeferred {
		t.Fatalf("%q HasDeferred = %t, want %t", spec.title, got.Health.HasDeferred, spec.wantDeferred)
	}
	if got.Health.HasSomeday != spec.wantSomeday {
		t.Fatalf("%q HasSomeday = %t, want %t", spec.title, got.Health.HasSomeday, spec.wantSomeday)
	}
	if got.Health.Stalled != spec.wantStalled {
		t.Fatalf("%q Stalled = %t, want %t", spec.title, got.Health.Stalled, spec.wantStalled)
	}
	assertProjectReviewChildCounts(t, got, spec)
}

func assertProjectReviewChildCounts(t *testing.T, got ProjectItemReview, spec projectReviewSpec) {
	t.Helper()
	switch spec.childState {
	case ItemStateNext:
		if got.Children.Next != 1 {
			t.Fatalf("%q child Next count = %d, want 1", spec.title, got.Children.Next)
		}
	case ItemStateWaiting:
		if got.Children.Waiting != 1 {
			t.Fatalf("%q child Waiting count = %d, want 1", spec.title, got.Children.Waiting)
		}
	case ItemStateDeferred:
		if got.Children.Deferred != 1 {
			t.Fatalf("%q child Deferred count = %d, want 1", spec.title, got.Children.Deferred)
		}
	case ItemStateSomeday:
		if got.Children.Someday != 1 {
			t.Fatalf("%q child Someday count = %d, want 1", spec.title, got.Children.Someday)
		}
	}
	if spec.title == "Outcome stalled" {
		if got.Children.Done != 1 {
			t.Fatalf("stalled outcome child Done count = %d, want 1", got.Children.Done)
		}
		if got.Children.Total != 1 {
			t.Fatalf("stalled outcome child Total = %d, want 1 (done child counts toward Total)", got.Children.Total)
		}
		return
	}
	if got.Children.Total != spec.wantTotal {
		t.Fatalf("%q child Total = %d, want %d", spec.title, got.Children.Total, spec.wantTotal)
	}
}

// TestListProjectItemReviewsKeepsSourceContainersOutOfReview pins the
// terminology contract from issue #728: Workspaces and external source
// containers (Todoist projects, GitHub Projects, mail folders) must never
// surface as project items unless explicitly captured as Item(kind=project).
func TestListProjectItemReviewsKeepsSourceContainersOutOfReview(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Daily review workspace", t.TempDir(), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	containerSource := ExternalProviderTodoist
	containerRef := "todoist-project-42"
	if _, err := s.CreateItem("Source container item", ItemOptions{
		Kind:      ItemKindAction,
		State:     ItemStateNext,
		Source:    &containerSource,
		SourceRef: &containerRef,
	}); err != nil {
		t.Fatalf("CreateItem(source container action) error: %v", err)
	}

	if _, err := s.CreateItem("Open outcome", ItemOptions{
		Kind:        ItemKindProject,
		State:       ItemStateNext,
		WorkspaceID: &workspace.ID,
	}); err != nil {
		t.Fatalf("CreateItem(project) error: %v", err)
	}

	reviews, err := s.ListProjectItemReviewsFiltered(ItemListFilter{})
	if err != nil {
		t.Fatalf("ListProjectItemReviewsFiltered() error: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("review len = %d, want exactly the project item (workspaces and source containers must not appear)", len(reviews))
	}
	if reviews[0].Item.Title != "Open outcome" {
		t.Fatalf("review surfaced %q, want %q", reviews[0].Item.Title, "Open outcome")
	}
	if reviews[0].Item.Kind != ItemKindProject {
		t.Fatalf("review surfaced kind %q, want %q", reviews[0].Item.Kind, ItemKindProject)
	}

	scoped, err := s.ListProjectItemReviewsFiltered(ItemListFilter{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("ListProjectItemReviewsFiltered(workspace) error: %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("workspace-scoped review len = %d, want 1 (workspace filter must scope project items, not turn workspaces into outcomes)", len(scoped))
	}
}

// TestListProjectItemReviewsPreservesSourceBackedBindings asserts the
// source-of-truth contract from issue #728: a source-backed project item must
// keep its Source/SourceRef pointing at the upstream system, and its action
// children retain their own bindings unchanged. The review surface only reads
// state — it never strips authority.
func TestListProjectItemReviewsPreservesSourceBackedBindings(t *testing.T) {
	s := newTestStore(t)

	parentSource := "github"
	parentRef := "krystophny/repo#728"
	parent, err := s.CreateItem("Source-backed outcome", ItemOptions{
		Kind:      ItemKindProject,
		State:     ItemStateNext,
		Source:    &parentSource,
		SourceRef: &parentRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(source-backed project) error: %v", err)
	}
	childSource := ExternalProviderTodoist
	childRef := "todoist-task-9"
	child, err := s.CreateItem("Source-backed child", ItemOptions{
		Kind:      ItemKindAction,
		State:     ItemStateNext,
		Source:    &childSource,
		SourceRef: &childRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(source-backed child) error: %v", err)
	}
	if err := s.LinkItemChild(parent.ID, child.ID, ItemLinkRoleNextAction); err != nil {
		t.Fatalf("LinkItemChild() error: %v", err)
	}

	reviews, err := s.ListProjectItemReviewsFiltered(ItemListFilter{})
	if err != nil {
		t.Fatalf("ListProjectItemReviewsFiltered() error: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("review len = %d, want 1", len(reviews))
	}
	got := reviews[0]
	if got.Item.Source == nil || *got.Item.Source != parentSource {
		t.Fatalf("project item Source = %v, want %q", got.Item.Source, parentSource)
	}
	if got.Item.SourceRef == nil || *got.Item.SourceRef != parentRef {
		t.Fatalf("project item SourceRef = %v, want %q", got.Item.SourceRef, parentRef)
	}
	if !got.Health.HasNextAction {
		t.Fatalf("HasNextAction = false, want true (child action backs the outcome)")
	}
	if got.Children.Next != 1 || got.Children.Total != 1 {
		t.Fatalf("child counts = %+v, want Next=1 Total=1", got.Children)
	}

	roundTrip, err := s.GetItemBySource(childSource, childRef)
	if err != nil {
		t.Fatalf("GetItemBySource(child) error: %v", err)
	}
	if roundTrip.ID != child.ID {
		t.Fatalf("child round-trip ID = %d, want %d", roundTrip.ID, child.ID)
	}
}

func mapKeys[V any](in map[string]V) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	return out
}
