package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/sloppy-org/slopshell/internal/store"
)

func TestBrainInvalidationTargetsLimitsWatchingToCommitments(t *testing.T) {
	root := t.TempDir()
	workBrain := filepath.Join(root, "work-brain")
	privateBrain := filepath.Join(root, "private-brain")
	mustMkdirAll(t, filepath.Join(workBrain, "commitments"))
	mustMkdirAll(t, filepath.Join(workBrain, "topics"))
	mustMkdirAll(t, filepath.Join(privateBrain, "commitments"))
	targets := brainInvalidationTargets(map[string]string{
		store.SphereWork:    workBrain,
		store.SpherePrivate: privateBrain,
	})
	want := map[string]bool{
		filepath.Join(workBrain, "commitments"):    true,
		filepath.Join(privateBrain, "commitments"): true,
	}
	if len(targets) != len(want) {
		t.Fatalf("target count = %d, want %d: %#v", len(targets), len(want), targets)
	}
	for _, target := range targets {
		if !want[target] {
			t.Fatalf("unexpected watch target %q in %#v", target, targets)
		}
		delete(want, target)
	}
	if len(want) != 0 {
		t.Fatalf("missing watch targets: %#v", want)
	}
}

func TestBrainInvalidationEventRelevantOnlyForMarkdownStateChanges(t *testing.T) {
	cases := []struct {
		name  string
		event fsnotify.Event
		want  bool
	}{
		{name: "markdown write", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.md", Op: fsnotify.Write}, want: true},
		{name: "markdown create", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.md", Op: fsnotify.Create}, want: true},
		{name: "markdown rename", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.md", Op: fsnotify.Rename}, want: true},
		{name: "markdown remove", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.md", Op: fsnotify.Remove}, want: true},
		{name: "markdown chmod", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.md", Op: fsnotify.Chmod}, want: false},
		{name: "non markdown write", event: fsnotify.Event{Name: "/tmp/brain/commitments/item.txt", Op: fsnotify.Write}, want: false},
	}
	for _, tc := range cases {
		if got := brainInvalidationEventRelevant(tc.event); got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
