package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const githubIssueListTimeout = 60 * time.Second

var githubIssueSourceRefPattern = regexp.MustCompile(`^([^#]+)#(\d+)$`)

type itemGitHubSyncRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type itemGitHubSyncResponse struct {
	OK          bool   `json:"ok"`
	WorkspaceID int64  `json:"workspace_id"`
	Repo        string `json:"repo"`
	Synced      int    `json:"synced"`
	Open        int    `json:"open"`
	Closed      int    `json:"closed"`
}

type ghIssueListLabel struct {
	Name string `json:"name"`
}

type ghIssueListAssignee struct {
	Login string `json:"login"`
}

type ghIssueListItem struct {
	Number    int                   `json:"number"`
	Title     string                `json:"title"`
	URL       string                `json:"url"`
	State     string                `json:"state"`
	Labels    []ghIssueListLabel    `json:"labels"`
	Assignees []ghIssueListAssignee `json:"assignees"`
}

func optionalTrimmedString(raw string) *string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	return &clean
}

func legacyBugReportIssueSourceRef(number int) string {
	return fmt.Sprintf("issue:%d", number)
}

func githubIssueSourceRef(ownerRepo string, number int) string {
	return fmt.Sprintf("%s#%d", strings.TrimSpace(ownerRepo), number)
}

func parseGitHubIssueSourceRef(raw string) (string, int, bool) {
	match := githubIssueSourceRefPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) != 3 {
		return "", 0, false
	}
	ownerRepo := strings.TrimSpace(match[1])
	if ownerRepo == "" {
		return "", 0, false
	}
	number := 0
	if _, err := fmt.Sscanf(match[2], "%d", &number); err != nil || number <= 0 {
		return "", 0, false
	}
	return ownerRepo, number, true
}

func parseLegacyBugReportIssueSourceRef(raw string) (int, bool) {
	number := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "issue:%d", &number); err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

func trimmedStringPointer(raw *string) string {
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(*raw)
}

func githubIssueItemState(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "open":
		return store.ItemStateInbox, nil
	case "closed":
		return store.ItemStateDone, nil
	default:
		return "", fmt.Errorf("unsupported github issue state %q", raw)
	}
}

func githubIssueArtifactMeta(ownerRepo string, issue ghIssueListItem) (*string, error) {
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if clean := strings.TrimSpace(label.Name); clean != "" {
			labels = append(labels, clean)
		}
	}
	assignees := make([]string, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		if clean := strings.TrimSpace(assignee.Login); clean != "" {
			assignees = append(assignees, clean)
		}
	}
	payload := map[string]any{
		"owner_repo": ownerRepo,
		"number":     issue.Number,
		"state":      strings.ToLower(strings.TrimSpace(issue.State)),
		"url":        strings.TrimSpace(issue.URL),
		"labels":     labels,
		"assignees":  assignees,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func githubIssueCommandDir(ownerRepo string, workspaceDirs []string) string {
	if repoRoot := resolveCanonicalGitHubRepoRoot(ownerRepo); repoRoot != "" {
		return repoRoot
	}
	for _, dir := range workspaceDirs {
		if root := resolveGitRepoRoot(dir); root != "" {
			return root
		}
	}
	return "."
}

func validateGitHubIssue(issue ghIssueListItem) error {
	if issue.Number <= 0 {
		return errors.New("github issue number is required")
	}
	if strings.TrimSpace(issue.Title) == "" {
		return fmt.Errorf("github issue #%d title is required", issue.Number)
	}
	return nil
}

func (a *App) listGitHubIssues(ctx context.Context, cwd, ownerRepo string) ([]ghIssueListItem, error) {
	runner := a.ghCommandRunner
	if runner == nil {
		runner = runGitHubCLI
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, githubIssueListTimeout)
	defer cancel()

	args := withGitHubRepoArg([]string{
		"issue", "list",
		"--state", "all",
		"--limit", "500",
		"--json", "number,title,url,state,labels,assignees",
	}, ownerRepo)
	raw, err := runner(
		ctx,
		cwd,
		args...,
	)
	if err != nil {
		return nil, err
	}
	var issues []ghIssueListItem
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		return nil, fmt.Errorf("invalid github issue response: %w", err)
	}
	return issues, nil
}

func (a *App) syncGitHubIssueArtifact(item store.Item, ownerRepo string, issue ghIssueListItem) error {
	title := strings.TrimSpace(issue.Title)
	kind := store.ArtifactKindGitHubIssue
	refURL := optionalTrimmedString(issue.URL)
	metaJSON, err := githubIssueArtifactMeta(ownerRepo, issue)
	if err != nil {
		return err
	}

	createArtifact := func() (store.Artifact, error) {
		return a.store.CreateArtifact(kind, nil, refURL, optionalTrimmedString(title), metaJSON)
	}

	if item.ArtifactID == nil {
		artifact, err := createArtifact()
		if err != nil {
			return err
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}

	err = a.store.UpdateArtifact(*item.ArtifactID, store.ArtifactUpdate{
		Kind:     &kind,
		RefURL:   refURL,
		Title:    optionalTrimmedString(title),
		MetaJSON: metaJSON,
	})
	if errors.Is(err, sql.ErrNoRows) {
		artifact, createErr := createArtifact()
		if createErr != nil {
			return createErr
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}
	return err
}

func (a *App) syncGitHubIssueState(sourceRef, currentState, remoteState string) (string, bool, error) {
	desiredState, err := githubIssueItemState(remoteState)
	if err != nil {
		return "", false, err
	}
	if currentState == desiredState {
		return desiredState, false, nil
	}
	switch desiredState {
	case store.ItemStateDone:
		if err := a.store.CompleteItemBySource("github", sourceRef); err != nil {
			return "", false, err
		}
	case store.ItemStateInbox:
		if err := a.store.SyncItemStateBySource("github", sourceRef, store.ItemStateInbox); err != nil {
			return "", false, err
		}
	default:
		return "", false, fmt.Errorf("unsupported item state %q", desiredState)
	}
	return desiredState, true, nil
}

func trackedGitHubIssueSource(item store.Item) (string, int, bool) {
	switch strings.ToLower(trimmedStringPointer(item.Source)) {
	case "github":
		return parseGitHubIssueSourceRef(trimmedStringPointer(item.SourceRef))
	case "bug_report":
		number, ok := parseLegacyBugReportIssueSourceRef(trimmedStringPointer(item.SourceRef))
		if !ok {
			return "", 0, false
		}
		return taburaBugReportOwnerRepo, number, true
	default:
		return "", 0, false
	}
}

func (a *App) syncTrackedGitHubIssueItem(item store.Item, ownerRepo string, issue ghIssueListItem) (bool, error) {
	if err := validateGitHubIssue(issue); err != nil {
		return false, err
	}

	sourceRef := githubIssueSourceRef(ownerRepo, issue.Number)
	changed := false
	if trimmedStringPointer(item.Source) != "github" || trimmedStringPointer(item.SourceRef) != sourceRef {
		if err := a.store.UpdateItemSource(item.ID, "github", sourceRef); err != nil {
			return false, err
		}
		item.Source = optionalTrimmedString("github")
		item.SourceRef = optionalTrimmedString(sourceRef)
		changed = true
	}

	originalTitle := strings.TrimSpace(item.Title)
	originalState := item.State
	hadArtifact := item.ArtifactID != nil

	updated, err := a.store.UpsertItemFromSource("github", sourceRef, issue.Title, item.WorkspaceID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(issue.Title) != originalTitle {
		changed = true
	}
	if !hadArtifact {
		changed = true
	}
	if err := a.syncGitHubIssueArtifact(updated, ownerRepo, issue); err != nil {
		return false, err
	}
	nextState, stateChanged, err := a.syncGitHubIssueState(sourceRef, updated.State, issue.State)
	if err != nil {
		return false, err
	}
	if stateChanged || nextState != originalState {
		changed = true
	}
	return changed, nil
}

func (a *App) findWorkspaceGitHubIssueItem(workspaceID int64, ownerRepo string, issueNumber int) (*store.Item, error) {
	sourceRef := githubIssueSourceRef(ownerRepo, issueNumber)
	item, err := a.store.GetItemBySource("github", sourceRef)
	if err == nil {
		return &item, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	legacy, err := a.store.GetItemBySource("bug_report", legacyBugReportIssueSourceRef(issueNumber))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if legacy.WorkspaceID == nil || *legacy.WorkspaceID != workspaceID {
		return nil, nil
	}
	return &legacy, nil
}

func (a *App) syncWorkspaceGitHubIssue(workspaceID int64, ownerRepo string, issue ghIssueListItem) (bool, error) {
	existing, err := a.findWorkspaceGitHubIssueItem(workspaceID, ownerRepo, issue.Number)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return a.syncTrackedGitHubIssueItem(*existing, ownerRepo, issue)
	}
	if err := validateGitHubIssue(issue); err != nil {
		return false, err
	}
	sourceRef := githubIssueSourceRef(ownerRepo, issue.Number)
	item, err := a.store.UpsertItemFromSource("github", sourceRef, issue.Title, &workspaceID)
	if err != nil {
		return false, err
	}
	if err := a.syncGitHubIssueArtifact(item, ownerRepo, issue); err != nil {
		return false, err
	}
	if _, _, err := a.syncGitHubIssueState(sourceRef, item.State, issue.State); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) trackedGitHubIssueItems(workspaceID *int64) ([]store.Item, error) {
	var all []store.Item
	for _, source := range []string{"github", "bug_report"} {
		items, err := a.store.ListItemsFiltered(store.ItemListFilter{Source: source})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	if workspaceID == nil {
		return all, nil
	}
	filtered := make([]store.Item, 0, len(all))
	for _, item := range all {
		if item.WorkspaceID == nil || *item.WorkspaceID != *workspaceID {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (a *App) syncTrackedGitHubIssues(ctx context.Context, workspaceID *int64, skipRepos map[string]struct{}) (int, error) {
	items, err := a.trackedGitHubIssueItems(workspaceID)
	if err != nil {
		return 0, err
	}
	type repoGroup struct {
		itemsByNumber map[int]store.Item
		workspaceDirs []string
	}
	repos := map[string]*repoGroup{}
	for _, item := range items {
		ownerRepo, number, ok := trackedGitHubIssueSource(item)
		if !ok {
			continue
		}
		if _, skip := skipRepos[ownerRepo]; skip {
			continue
		}
		group := repos[ownerRepo]
		if group == nil {
			group = &repoGroup{itemsByNumber: map[int]store.Item{}}
			repos[ownerRepo] = group
		}
		group.itemsByNumber[number] = item
		if item.WorkspaceID == nil {
			continue
		}
		workspace, err := a.store.GetWorkspace(*item.WorkspaceID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return 0, err
		}
		if dir := strings.TrimSpace(workspace.DirPath); dir != "" {
			group.workspaceDirs = append(group.workspaceDirs, dir)
		}
	}

	changed := 0
	for ownerRepo, group := range repos {
		issues, err := a.listGitHubIssues(ctx, githubIssueCommandDir(ownerRepo, group.workspaceDirs), ownerRepo)
		if err != nil {
			return 0, err
		}
		for _, issue := range issues {
			item, ok := group.itemsByNumber[issue.Number]
			if !ok {
				continue
			}
			itemChanged, err := a.syncTrackedGitHubIssueItem(item, ownerRepo, issue)
			if err != nil {
				return 0, err
			}
			if itemChanged {
				changed++
			}
		}
	}
	return changed, nil
}

func (a *App) syncGitHubIssues(workspaceID int64) (itemGitHubSyncResponse, error) {
	workspace, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return itemGitHubSyncResponse{}, err
	}
	repo, err := a.store.GitHubRepoForWorkspace(workspaceID)
	if err != nil {
		return itemGitHubSyncResponse{}, err
	}
	if strings.TrimSpace(repo) == "" {
		return itemGitHubSyncResponse{}, errors.New("workspace has no GitHub origin remote")
	}

	issues, err := a.listGitHubIssues(context.Background(), workspace.DirPath, repo)
	if err != nil {
		return itemGitHubSyncResponse{}, err
	}

	result := itemGitHubSyncResponse{
		OK:          true,
		WorkspaceID: workspace.ID,
		Repo:        repo,
		Synced:      len(issues),
	}
	for _, issue := range issues {
		if err := validateGitHubIssue(issue); err != nil {
			return itemGitHubSyncResponse{}, err
		}
		if _, err := a.syncWorkspaceGitHubIssue(workspace.ID, repo, issue); err != nil {
			return itemGitHubSyncResponse{}, err
		}
		switch strings.ToLower(strings.TrimSpace(issue.State)) {
		case "closed":
			result.Closed++
		case "open":
			result.Open++
		default:
			return itemGitHubSyncResponse{}, fmt.Errorf("unsupported github issue state %q", issue.State)
		}
	}

	if _, err := a.syncTrackedGitHubIssues(context.Background(), &workspace.ID, map[string]struct{}{repo: {}}); err != nil {
		return itemGitHubSyncResponse{}, err
	}
	return result, nil
}

func (a *App) handleGitHubIssueSync(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req itemGitHubSyncRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID <= 0 {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	result, err := a.syncGitHubIssues(req.WorkspaceID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, err.Error(), http.StatusNotFound)
		case strings.Contains(strings.ToLower(err.Error()), "no github origin remote"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	writeJSON(w, result)
}
