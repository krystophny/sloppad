package store

import (
	"strings"
	"testing"
)

func TestStoreItemChildLinkLifecycleAndProjectHealth(t *testing.T) {
	s := newTestStore(t)

	project, err := s.CreateItem("Ship GTD outcome", ItemOptions{Kind: ItemKindProject})
	if err != nil {
		t.Fatalf("CreateItem(project) error: %v", err)
	}
	if project.Kind != ItemKindProject {
		t.Fatalf("project kind = %q, want %q", project.Kind, ItemKindProject)
	}

	source := ExternalProviderTodoist
	sourceRef := "task-1"
	nextAction, err := s.CreateItem("Source-backed next action", ItemOptions{
		Kind:      ItemKindAction,
		State:     ItemStateNext,
		Source:    &source,
		SourceRef: &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(source-backed) error: %v", err)
	}
	if nextAction.Kind != ItemKindAction {
		t.Fatalf("next action kind = %q, want %q", nextAction.Kind, ItemKindAction)
	}
	if nextAction.Source == nil || nextAction.SourceRef == nil || *nextAction.Source != source || *nextAction.SourceRef != sourceRef {
		t.Fatalf("source-backed item bindings lost: %+v", nextAction)
	}
	sourceRoundTrip, err := s.GetItemBySource(source, sourceRef)
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if sourceRoundTrip.ID != nextAction.ID {
		t.Fatalf("GetItemBySource() = %d, want %d", sourceRoundTrip.ID, nextAction.ID)
	}

	waitingChild, err := s.CreateItem("Waiting support", ItemOptions{State: ItemStateWaiting})
	if err != nil {
		t.Fatalf("CreateItem(waiting) error: %v", err)
	}
	deferredChild, err := s.CreateItem("Deferred blocker", ItemOptions{State: ItemStateDeferred})
	if err != nil {
		t.Fatalf("CreateItem(deferred) error: %v", err)
	}
	somedayChild, err := s.CreateItem("Someday support", ItemOptions{State: ItemStateSomeday})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	actionParent, err := s.CreateItem("Plain action", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(action parent) error: %v", err)
	}

	if err := s.LinkItemChild(actionParent.ID, nextAction.ID, ItemLinkRoleNextAction); err == nil || !strings.Contains(err.Error(), "parent item must be a project") {
		t.Fatalf("LinkItemChild(action parent) error = %v, want project parent rejection", err)
	}

	for _, tc := range []struct {
		child int64
		role  string
	}{
		{child: nextAction.ID, role: ItemLinkRoleNextAction},
		{child: waitingChild.ID, role: ItemLinkRoleSupport},
		{child: deferredChild.ID, role: ItemLinkRoleBlockedBy},
		{child: somedayChild.ID, role: ItemLinkRoleSupport},
	} {
		if err := s.LinkItemChild(project.ID, tc.child, tc.role); err != nil {
			t.Fatalf("LinkItemChild(%d,%d,%s) error: %v", project.ID, tc.child, tc.role, err)
		}
	}

	links, err := s.ListItemChildLinks(project.ID)
	if err != nil {
		t.Fatalf("ListItemChildLinks() error: %v", err)
	}
	if len(links) != 4 {
		t.Fatalf("ListItemChildLinks() len = %d, want 4", len(links))
	}
	seen := map[int64]string{}
	for _, link := range links {
		if link.ParentItemID != project.ID {
			t.Fatalf("link parent = %d, want %d", link.ParentItemID, project.ID)
		}
		seen[link.ChildItemID] = link.Role
	}
	if seen[nextAction.ID] != ItemLinkRoleNextAction || seen[waitingChild.ID] != ItemLinkRoleSupport || seen[deferredChild.ID] != ItemLinkRoleBlockedBy || seen[somedayChild.ID] != ItemLinkRoleSupport {
		t.Fatalf("ListItemChildLinks() roles = %#v", seen)
	}

	health, err := s.GetProjectItemHealth(project.ID)
	if err != nil {
		t.Fatalf("GetProjectItemHealth() error: %v", err)
	}
	if !health.HasNextAction || !health.HasWaiting || !health.HasDeferred || !health.HasSomeday || health.Stalled {
		t.Fatalf("GetProjectItemHealth() = %+v, want all health flags set and stalled false", health)
	}

	for _, childID := range []int64{nextAction.ID, waitingChild.ID, deferredChild.ID, somedayChild.ID} {
		if err := s.UnlinkItemChild(project.ID, childID); err != nil {
			t.Fatalf("UnlinkItemChild(%d) error: %v", childID, err)
		}
	}
	links, err = s.ListItemChildLinks(project.ID)
	if err != nil {
		t.Fatalf("ListItemChildLinks(after unlink) error: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("ListItemChildLinks(after unlink) len = %d, want 0", len(links))
	}

	health, err = s.GetProjectItemHealth(project.ID)
	if err != nil {
		t.Fatalf("GetProjectItemHealth(after unlink) error: %v", err)
	}
	if health.Stalled != true || health.HasNextAction || health.HasWaiting || health.HasDeferred || health.HasSomeday {
		t.Fatalf("GetProjectItemHealth(after unlink) = %+v, want stalled true and no health flags", health)
	}
}
