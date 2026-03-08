package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestGitHubIssueSyncAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	repoDir := filepath.Join(t.TempDir(), "workspace")
	initGitHubWorkspaceRepo(t, repoDir, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Repo", repoDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	var calls [][]string
	var callCWDs []string
	callCount := 0
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		callCWDs = append(callCWDs, cwd)
		callCount++
		switch callCount {
		case 1:
			return `[
				{"number":12,"title":"Open bug","url":"https://github.com/owner/tabula/issues/12","state":"OPEN","labels":[{"name":"bug"}],"assignees":[{"login":"octocat"}]},
				{"number":13,"title":"Closed task","url":"https://github.com/owner/tabula/issues/13","state":"CLOSED","labels":[],"assignees":[]}
			]`, nil
		case 2:
			return `[
				{"number":12,"title":"Open bug renamed","url":"https://github.com/owner/tabula/issues/12","state":"OPEN","labels":[{"name":"bug"}],"assignees":[{"login":"octocat"}]},
				{"number":13,"title":"Closed task reopened","url":"https://github.com/owner/tabula/issues/13","state":"OPEN","labels":[{"name":"help wanted"}],"assignees":[]}
			]`, nil
		default:
			t.Fatalf("unexpected gh invocation %d", callCount)
			return "", nil
		}
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/sync/github", map[string]any{
		"workspace_id": workspace.ID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("first sync status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var response itemGitHubSyncResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode first sync response: %v", err)
	}
	if response.Synced != 2 || response.Open != 1 || response.Closed != 1 {
		t.Fatalf("first sync response = %+v, want synced=2 open=1 closed=1", response)
	}

	openItem, err := app.store.GetItemBySource("github", "owner/tabula#12")
	if err != nil {
		t.Fatalf("GetItemBySource(open) error: %v", err)
	}
	if openItem.State != store.ItemStateInbox {
		t.Fatalf("open item state = %q, want %q", openItem.State, store.ItemStateInbox)
	}
	if openItem.WorkspaceID == nil || *openItem.WorkspaceID != workspace.ID {
		t.Fatalf("open item workspace = %v, want %d", openItem.WorkspaceID, workspace.ID)
	}
	if openItem.ArtifactID == nil {
		t.Fatal("expected open item artifact")
	}
	openArtifact, err := app.store.GetArtifact(*openItem.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(open) error: %v", err)
	}
	if openArtifact.Kind != store.ArtifactKindGitHubIssue {
		t.Fatalf("open artifact kind = %q, want %q", openArtifact.Kind, store.ArtifactKindGitHubIssue)
	}
	if openArtifact.RefURL == nil || *openArtifact.RefURL != "https://github.com/owner/tabula/issues/12" {
		t.Fatalf("open artifact ref_url = %v, want issue URL", openArtifact.RefURL)
	}
	var openMeta map[string]any
	if openArtifact.MetaJSON == nil {
		t.Fatal("expected open artifact meta_json")
	}
	if err := json.Unmarshal([]byte(*openArtifact.MetaJSON), &openMeta); err != nil {
		t.Fatalf("unmarshal open artifact meta_json: %v", err)
	}
	if openMeta["owner_repo"] != "owner/tabula" {
		t.Fatalf("open artifact owner_repo = %v, want owner/tabula", openMeta["owner_repo"])
	}
	if openMeta["state"] != "open" {
		t.Fatalf("open artifact state = %v, want open", openMeta["state"])
	}

	closedItem, err := app.store.GetItemBySource("github", "owner/tabula#13")
	if err != nil {
		t.Fatalf("GetItemBySource(closed) error: %v", err)
	}
	if closedItem.State != store.ItemStateDone {
		t.Fatalf("closed item state = %q, want %q", closedItem.State, store.ItemStateDone)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/sync/github", map[string]any{
		"workspace_id": workspace.ID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("second sync status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	openItem, err = app.store.GetItemBySource("github", "owner/tabula#12")
	if err != nil {
		t.Fatalf("GetItemBySource(open, second sync) error: %v", err)
	}
	if openItem.Title != "Open bug renamed" {
		t.Fatalf("open item title after resync = %q, want %q", openItem.Title, "Open bug renamed")
	}
	reopenedItem, err := app.store.GetItemBySource("github", "owner/tabula#13")
	if err != nil {
		t.Fatalf("GetItemBySource(reopened) error: %v", err)
	}
	if reopenedItem.ID != closedItem.ID {
		t.Fatalf("reopened item ID = %d, want %d", reopenedItem.ID, closedItem.ID)
	}
	if reopenedItem.State != store.ItemStateInbox {
		t.Fatalf("reopened item state = %q, want %q", reopenedItem.State, store.ItemStateInbox)
	}
	inboxItems, err := app.store.ListItemsByState(store.ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(inboxItems) != 2 {
		t.Fatalf("ListItemsByState(inbox) len = %d, want 2", len(inboxItems))
	}

	if len(calls) != 2 {
		t.Fatalf("gh call count = %d, want 2", len(calls))
	}
	for i, args := range calls {
		if callCWDs[i] != repoDir {
			t.Fatalf("gh cwd[%d] = %q, want %q", i, callCWDs[i], repoDir)
		}
		command := strings.Join(args, " ")
		if !strings.Contains(command, "issue list --state all --limit 500") {
			t.Fatalf("gh args[%d] = %q, want issue list all", i, command)
		}
		if !strings.Contains(command, "--json number,title,url,state,labels,assignees") {
			t.Fatalf("gh args[%d] = %q, want expected json fields", i, command)
		}
	}
}

func TestGitHubIssueSyncAPIRejectsWorkspaceWithoutGitHubRemote(t *testing.T) {
	app := newAuthedTestApp(t)

	workspaceDir := filepath.Join(t.TempDir(), "workspace")
	if err := exec.Command("git", "init", workspaceDir).Run(); err != nil {
		t.Fatalf("git init %s: %v", workspaceDir, err)
	}
	workspace, err := app.store.CreateWorkspace("Repo", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/sync/github", map[string]any{
		"workspace_id": workspace.ID,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sync without remote status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
}

func initGitHubWorkspaceRepo(t *testing.T, dirPath, remoteURL string) {
	t.Helper()
	if err := exec.Command("git", "init", dirPath).Run(); err != nil {
		t.Fatalf("git init %s: %v", dirPath, err)
	}
	if err := exec.Command("git", "-C", dirPath, "remote", "add", "origin", remoteURL).Run(); err != nil {
		t.Fatalf("git remote add origin %s: %v", dirPath, err)
	}
}
