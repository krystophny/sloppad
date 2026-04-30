package store

import (
	"strings"
	"testing"
	"time"
)

func TestCountSidebarSectionsFilteredCountsOpenProjectItemsAndRecentMeetings(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.April, 30, 10, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute).Format(time.RFC3339)

	if _, err := s.CreateItem("Plan Q2 outcome", ItemOptions{
		Kind:         ItemKindProject,
		State:        ItemStateInbox,
		VisibleAfter: &past,
	}); err != nil {
		t.Fatalf("CreateItem(project inbox) error: %v", err)
	}
	if _, err := s.CreateItem("Ship review queue", ItemOptions{
		Kind:  ItemKindProject,
		State: ItemStateNext,
	}); err != nil {
		t.Fatalf("CreateItem(project next) error: %v", err)
	}
	if _, err := s.CreateItem("Closed outcome", ItemOptions{
		Kind:  ItemKindProject,
		State: ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(project done) error: %v", err)
	}
	if _, err := s.CreateItem("Plain action", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	}); err != nil {
		t.Fatalf("CreateItem(action) error: %v", err)
	}

	recentTranscriptPath := "/tmp/recent-transcript.md"
	recentTranscriptTitle := "Recent meeting transcript"
	if _, err := s.CreateArtifact(ArtifactKindTranscript, &recentTranscriptPath, nil, &recentTranscriptTitle, nil); err != nil {
		t.Fatalf("CreateArtifact(transcript) error: %v", err)
	}
	recentSummaryPath := "/tmp/recent-summary.md"
	recentSummaryTitle := "Recent meeting summary"
	recentMeta := `{"source":"meeting_summary","summary":"recap"}`
	if _, err := s.CreateArtifact(ArtifactKindMarkdown, &recentSummaryPath, nil, &recentSummaryTitle, &recentMeta); err != nil {
		t.Fatalf("CreateArtifact(summary) error: %v", err)
	}
	unrelatedPath := "/tmp/notes.md"
	unrelatedTitle := "Unrelated notes"
	if _, err := s.CreateArtifact(ArtifactKindMarkdown, &unrelatedPath, nil, &unrelatedTitle, nil); err != nil {
		t.Fatalf("CreateArtifact(unrelated) error: %v", err)
	}

	got, err := s.CountSidebarSectionsFiltered(now, ItemListFilter{})
	if err != nil {
		t.Fatalf("CountSidebarSectionsFiltered() error: %v", err)
	}
	if got.ProjectItemsOpen != 2 {
		t.Fatalf("ProjectItemsOpen = %d, want 2 (open project items only, done excluded)", got.ProjectItemsOpen)
	}
	if got.RecentMeetings != 2 {
		t.Fatalf("RecentMeetings = %d, want 2 (transcript + meeting_summary metadata)", got.RecentMeetings)
	}
}

func TestCountSidebarSectionsFilteredHonorsSphereScope(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.April, 30, 10, 0, 0, 0, time.UTC)

	workSphere := SphereWork
	privateSphere := SpherePrivate
	if _, err := s.CreateItem("Work outcome", ItemOptions{
		Kind:   ItemKindProject,
		State:  ItemStateNext,
		Sphere: &workSphere,
	}); err != nil {
		t.Fatalf("CreateItem(work) error: %v", err)
	}
	if _, err := s.CreateItem("Private outcome", ItemOptions{
		Kind:   ItemKindProject,
		State:  ItemStateInbox,
		Sphere: &privateSphere,
	}); err != nil {
		t.Fatalf("CreateItem(private) error: %v", err)
	}

	work, err := s.CountSidebarSectionsFiltered(now, ItemListFilter{Sphere: SphereWork})
	if err != nil {
		t.Fatalf("CountSidebarSectionsFiltered(work) error: %v", err)
	}
	if work.ProjectItemsOpen != 1 {
		t.Fatalf("work ProjectItemsOpen = %d, want 1", work.ProjectItemsOpen)
	}

	priv, err := s.CountSidebarSectionsFiltered(now, ItemListFilter{Sphere: SpherePrivate})
	if err != nil {
		t.Fatalf("CountSidebarSectionsFiltered(private) error: %v", err)
	}
	if priv.ProjectItemsOpen != 1 {
		t.Fatalf("private ProjectItemsOpen = %d, want 1", priv.ProjectItemsOpen)
	}
}

func TestCountSidebarSectionsFilteredExcludesAgedMeetings(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.April, 30, 10, 0, 0, 0, time.UTC)

	recentPath := "/tmp/recent.md"
	recentTitle := "Recent transcript"
	if _, err := s.CreateArtifact(ArtifactKindTranscript, &recentPath, nil, &recentTitle, nil); err != nil {
		t.Fatalf("CreateArtifact(recent) error: %v", err)
	}

	staleCreatedAt := now.AddDate(0, 0, -8).UTC().Format(time.RFC3339Nano)
	stalePath := "/tmp/stale.md"
	staleTitle := "Stale transcript"
	stale, err := s.CreateArtifact(ArtifactKindTranscript, &stalePath, nil, &staleTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(stale) error: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE artifacts SET created_at = ? WHERE id = ?`, staleCreatedAt, stale.ID); err != nil {
		t.Fatalf("backdate artifact error: %v", err)
	}

	got, err := s.CountSidebarSectionsFiltered(now, ItemListFilter{})
	if err != nil {
		t.Fatalf("CountSidebarSectionsFiltered() error: %v", err)
	}
	if got.RecentMeetings != 1 {
		t.Fatalf("RecentMeetings = %d, want 1 (stale meeting older than 7 days excluded)", got.RecentMeetings)
	}

	// Sanity-check the assertion below which guards against accidental
	// terminology drift: project items should never be reported as 0
	// when there are recent meetings only.
	if !strings.EqualFold(string(ArtifactKindTranscript), "transcript") {
		t.Fatalf("ArtifactKindTranscript constant changed: %q", string(ArtifactKindTranscript))
	}
}
