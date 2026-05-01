package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestWorkspaceMarkdownLinkPanelOutgoingAndBroken(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir topics: %v", err)
	}
	body := strings.Join([]string{
		"# Active",
		"",
		"See [related](related.md) and [missing](does-not-exist.md).",
		"Also [[Related Note]] for the wikilink path.",
		"```",
		"[fenced](should-not-count.md)",
		"```",
		"And `[inline](skip.md)` inline code is also ignored.",
	}, "\n")
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "related.md"), []byte("related"), 0o644); err != nil {
		t.Fatalf("write related: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "Related Note.md"), []byte("related note"), 0o644); err != nil {
		t.Fatalf("write related note: %v", err)
	}

	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	panel := requestMarkdownLinkPanel(t, app, workspace.ID, "topics/active.md")
	if !panel.OK || panel.SourcePath != "topics/active.md" {
		t.Fatalf("panel = %+v", panel)
	}
	if got := len(panel.Outgoing); got != 3 {
		t.Fatalf("outgoing count = %d, want 3 (fenced/inline-code links must be ignored): %+v", got, panel.Outgoing)
	}
	byTarget := map[string]workspaceMarkdownLinkRef{}
	for _, ref := range panel.Outgoing {
		byTarget[ref.Type+":"+ref.Target] = ref
	}
	related, ok := byTarget["markdown:related.md"]
	if !ok || !related.OK || related.ResolvedPath != "brain/topics/related.md" || related.Kind != "text" {
		t.Fatalf("related ref = %+v", related)
	}
	missing, ok := byTarget["markdown:does-not-exist.md"]
	if !ok || missing.OK || !missing.Blocked || missing.Reason != "link target was not found in the vault" {
		t.Fatalf("missing ref = %+v", missing)
	}
	wiki, ok := byTarget["wikilink:Related Note"]
	if !ok || !wiki.OK || wiki.ResolvedPath != "brain/topics/Related Note.md" {
		t.Fatalf("wiki ref = %+v", wiki)
	}
	if panel.BrokenCount != 1 {
		t.Fatalf("broken count = %d, want 1", panel.BrokenCount)
	}
}

func TestWorkspaceMarkdownLinkPanelBacklinksAndPersonalExclusion(t *testing.T) {
	vaultRoot, personalRoot := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir topics: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(brainRoot, "people"), 0o755); err != nil {
		t.Fatalf("mkdir people: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("# Active"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(brainRoot, "people", "alice.md"),
		[]byte("Alice mentions [the topic](../topics/active.md) in their note."),
		0o644,
	); err != nil {
		t.Fatalf("write alice: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(brainRoot, "people", "bob.md"),
		[]byte("Bob refers to [[active]] explicitly."),
		0o644,
	); err != nil {
		t.Fatalf("write bob: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(brainRoot, "people", "carol.md"),
		[]byte("Carol points elsewhere [[unrelated]]."),
		0o644,
	); err != nil {
		t.Fatalf("write carol: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(personalRoot, "diary.md"),
		[]byte("private mention of [active](../brain/topics/active.md)"),
		0o644,
	); err != nil {
		t.Fatalf("write personal: %v", err)
	}

	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	panel := requestMarkdownLinkPanel(t, app, workspace.ID, "topics/active.md")
	if !panel.OK {
		t.Fatalf("panel not ok: %+v", panel)
	}
	if got := len(panel.Backlinks); got != 2 {
		t.Fatalf("backlinks = %d, want 2 (alice + bob, personal excluded): %+v", got, panel.Backlinks)
	}
	bySource := map[string]workspaceMarkdownBacklink{}
	for _, link := range panel.Backlinks {
		bySource[link.SourcePath] = link
	}
	alice, ok := bySource["brain/people/alice.md"]
	if !ok || alice.LinkType != "markdown" || alice.LinkTarget != "../topics/active.md" {
		t.Fatalf("alice backlink = %+v", alice)
	}
	if alice.Excerpt == "" || !strings.Contains(alice.Excerpt, "Alice mentions") {
		t.Fatalf("alice excerpt = %q", alice.Excerpt)
	}
	wantAliceURL := "/api/workspaces/" + itoa(workspace.ID) + "/markdown-link/file?path=brain%2Fpeople%2Falice.md"
	if alice.FileURL != wantAliceURL {
		t.Fatalf("alice file_url = %q, want %q", alice.FileURL, wantAliceURL)
	}
	bob, ok := bySource["brain/people/bob.md"]
	if !ok || bob.LinkType != "wikilink" || bob.LinkTarget != "active" {
		t.Fatalf("bob backlink = %+v", bob)
	}
	if bob.FileURL == "" || !strings.Contains(bob.FileURL, "brain%2Fpeople%2Fbob.md") {
		t.Fatalf("bob file_url = %q", bob.FileURL)
	}
	for _, link := range panel.Backlinks {
		if strings.Contains(link.SourcePath, "personal/") {
			t.Fatalf("backlink leaked personal subtree: %+v", link)
		}
		if strings.Contains(link.SourcePath, vaultRoot) || filepath.IsAbs(link.SourcePath) {
			t.Fatalf("backlink leaked absolute path: %+v", link)
		}
	}
}

func TestWorkspaceMarkdownLinkPanelBacklinkFileURLServesNote(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir topics: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(brainRoot, "people"), 0o755); err != nil {
		t.Fatalf("mkdir people: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("# Active"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	aliceBody := "Alice mentions [the topic](../topics/active.md)."
	if err := os.WriteFile(filepath.Join(brainRoot, "people", "alice.md"), []byte(aliceBody), 0o644); err != nil {
		t.Fatalf("write alice: %v", err)
	}

	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	panel := requestMarkdownLinkPanel(t, app, workspace.ID, "topics/active.md")
	if !panel.OK || len(panel.Backlinks) != 1 {
		t.Fatalf("panel = %+v", panel)
	}
	fileURL := panel.Backlinks[0].FileURL
	if fileURL == "" {
		t.Fatalf("backlink missing file_url: %+v", panel.Backlinks[0])
	}
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, fileURL, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("file_url GET status = %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != aliceBody {
		t.Fatalf("file_url body = %q, want %q", got, aliceBody)
	}
}

func TestWorkspaceMarkdownLinkPanelBlockedPersonalTarget(t *testing.T) {
	vaultRoot, personalRoot := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir topics: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(brainRoot, "topics", "active.md"),
		[]byte("Reference [diary](../../personal/diary.md)."),
		0o644,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personalRoot, "diary.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write personal: %v", err)
	}

	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	panel := requestMarkdownLinkPanel(t, app, workspace.ID, "topics/active.md")
	if !panel.OK || len(panel.Outgoing) != 1 {
		t.Fatalf("panel = %+v", panel)
	}
	ref := panel.Outgoing[0]
	if ref.OK || !ref.Blocked || !strings.Contains(ref.Reason, "work personal subtree is blocked") {
		t.Fatalf("blocked ref = %+v", ref)
	}
	if strings.Contains(ref.Reason, personalRoot) {
		t.Fatalf("reason leaked absolute path: %q", ref.Reason)
	}
	if panel.BrokenCount != 1 {
		t.Fatalf("broken count = %d, want 1", panel.BrokenCount)
	}
}

func TestWorkspaceMarkdownLinkPanelMissingSourceReportsError(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	if err := os.MkdirAll(brainRoot, 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}

	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	panel := requestMarkdownLinkPanel(t, app, workspace.ID, "topics/active.md")
	if panel.OK || panel.Error != "source note could not be read" {
		t.Fatalf("missing-source panel = %+v", panel)
	}
}

func requestMarkdownLinkPanel(t *testing.T, app *App, workspaceID int64, sourcePath string) workspaceMarkdownLinkPanel {
	t.Helper()
	values := url.Values{}
	values.Set("source", sourcePath)
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/workspaces/"+itoa(workspaceID)+"/markdown-link/panel?"+values.Encode(),
		nil,
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("panel status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var panel workspaceMarkdownLinkPanel
	if err := json.Unmarshal(rr.Body.Bytes(), &panel); err != nil {
		t.Fatalf("decode panel: %v", err)
	}
	return panel
}
