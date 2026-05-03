package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

type trackFocusRequest struct {
	Sphere        string `json:"sphere"`
	Track         string `json:"track"`
	ProjectItemID int64  `json:"project_item_id"`
	ActionItemID  int64  `json:"action_item_id"`
}

type trackFocusSnapshot struct {
	Sphere            string                    `json:"sphere"`
	Track             string                    `json:"track"`
	ProjectItem       *store.ItemSummary        `json:"project_item,omitempty"`
	ActionItem        *store.ItemSummary        `json:"action_item,omitempty"`
	WorkspaceID       *int64                    `json:"workspace_id,omitempty"`
	NeedsChoice       bool                      `json:"needs_choice"`
	CandidateProjects []store.ProjectItemReview `json:"candidate_projects,omitempty"`
	CandidateActions  []store.ItemSummary       `json:"candidate_actions,omitempty"`
}

type brainGTDFocusResponse struct {
	Focus brainGTDFocus `json:"focus"`
}

type brainGTDFocus struct {
	Sphere  string           `json:"sphere"`
	Track   string           `json:"track"`
	Project brainGTDFocusRef `json:"project"`
	Action  brainGTDFocusRef `json:"action"`
}

type brainGTDFocusRef struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Path   string `json:"path"`
}

var brainGTDFocusCall = defaultBrainGTDFocusCall

func (a *App) handleActiveTrackGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sphere := strings.TrimSpace(r.URL.Query().Get("sphere"))
	if sphere == "" {
		sphere = a.runtimeActiveSphere()
	}
	snapshot, _, err := a.activeTrackFocusSnapshot(r.Context(), sphere)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"focus": snapshot})
}

func (a *App) handleActiveTrackPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req trackFocusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sphere := normalizeRuntimeActiveSphere(req.Sphere)
	if sphere == "" {
		sphere = a.runtimeActiveSphere()
	}
	if _, err := writeBrainGTDFocus(r.Context(), sphere, map[string]interface{}{"track": strings.TrimSpace(req.Track)}); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if err := a.setActiveSphereTracked(sphere, "track_switch"); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.writeActiveTrackFocus(w, sphere, "track_switch")
}

func (a *App) handleActiveTrackProjectPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req trackFocusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sphere := fallbackSphere(req.Sphere, a.runtimeActiveSphere())
	project, err := a.itemFocusRef(req.ProjectItemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	args := map[string]interface{}{"project_source": project.Source, "project_ref": project.Ref, "project_path": project.Path}
	if track := strings.TrimSpace(req.Track); track != "" {
		args["track"] = track
	}
	if _, err := writeBrainGTDFocus(r.Context(), sphere, args); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.writeActiveTrackFocus(w, sphere, "project_switch")
}

func (a *App) handleActiveTrackActionPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req trackFocusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sphere := fallbackSphere(req.Sphere, a.runtimeActiveSphere())
	project, err := a.itemFocusRef(req.ProjectItemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	action, err := a.itemFocusRef(req.ActionItemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	args := map[string]interface{}{
		"project_source": project.Source,
		"project_ref":    project.Ref,
		"project_path":   project.Path,
		"action_source":  action.Source,
		"action_ref":     action.Ref,
		"action_path":    action.Path,
	}
	if track := strings.TrimSpace(req.Track); track != "" {
		args["track"] = track
	}
	if _, err := writeBrainGTDFocus(r.Context(), sphere, args); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.writeActiveTrackFocus(w, sphere, "action_switch")
}

func (a *App) writeActiveTrackFocus(w http.ResponseWriter, sphere, activity string) {
	snapshot, focus, err := a.activeTrackFocusSnapshot(context.Background(), sphere)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if snapshot.WorkspaceID != nil {
		if err := a.store.SetActiveWorkspace(*snapshot.WorkspaceID); err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}
	if _, _, err := a.store.SwitchActiveTimeEntryWithFocus(time.Now().UTC(), focus, activity, nil); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"focus": snapshot})
}

func (a *App) activeTrackFocusSnapshot(ctx context.Context, sphere string) (trackFocusSnapshot, store.TimeEntryFocus, error) {
	cleanSphere := normalizeRuntimeActiveSphere(sphere)
	if cleanSphere == "" {
		return trackFocusSnapshot{}, store.TimeEntryFocus{}, errors.New("sphere must be work or private")
	}
	focusState, err := readBrainGTDFocus(ctx, cleanSphere)
	if err != nil {
		return trackFocusSnapshot{}, store.TimeEntryFocus{}, err
	}
	track := strings.TrimSpace(focusState.Track)
	snapshot := trackFocusSnapshot{Sphere: cleanSphere, Track: track}
	project := a.rememberedTrackProject(cleanSphere, track, focusState.Project)
	if project == nil {
		snapshot.NeedsChoice = true
		snapshot.CandidateProjects, _ = a.store.ListProjectItemReviewsFiltered(store.ItemListFilter{Sphere: cleanSphere, Track: track})
		return snapshot, trackFocusToTimeEntryFocus(snapshot), nil
	}
	snapshot.ProjectItem = project
	action := a.rememberedTrackAction(cleanSphere, track, project.ID, focusState.Action)
	if action == nil {
		snapshot.NeedsChoice = true
		snapshot.CandidateActions, _ = a.store.ListNextItemsFiltered(store.ItemListFilter{Sphere: cleanSphere, Track: track, ProjectItemID: &project.ID})
	} else {
		snapshot.ActionItem = action
	}
	snapshot.WorkspaceID = focusWorkspaceID(project, action)
	return snapshot, trackFocusToTimeEntryFocus(snapshot), nil
}

func (a *App) rememberedTrackProject(sphere, track string, ref brainGTDFocusRef) *store.ItemSummary {
	item, err := a.focusRefItem(ref)
	if err != nil {
		return nil
	}
	if item.Kind != store.ItemKindProject || item.State == store.ItemStateDone {
		return nil
	}
	if !itemMatchesTrackAndSphere(item.Item, sphere, track) {
		return nil
	}
	return item
}

func (a *App) rememberedTrackAction(sphere, track string, projectID int64, ref brainGTDFocusRef) *store.ItemSummary {
	item, err := a.focusRefItem(ref)
	if err != nil {
		return nil
	}
	candidates, err := a.store.ListNextItemsFiltered(store.ItemListFilter{Sphere: sphere, Track: track, ProjectItemID: &projectID})
	if err != nil {
		return nil
	}
	for i := range candidates {
		if candidates[i].ID == item.ID && itemMatchesTrackAndSphere(candidates[i].Item, sphere, track) {
			return &candidates[i]
		}
	}
	return nil
}

func trackFocusToTimeEntryFocus(snapshot trackFocusSnapshot) store.TimeEntryFocus {
	focus := store.TimeEntryFocus{WorkspaceID: snapshot.WorkspaceID, Sphere: snapshot.Sphere, Track: snapshot.Track}
	if snapshot.ProjectItem != nil {
		focus.ProjectItemID = &snapshot.ProjectItem.ID
	}
	if snapshot.ActionItem != nil {
		focus.ActionItemID = &snapshot.ActionItem.ID
	}
	return focus
}

func focusWorkspaceID(project, action *store.ItemSummary) *int64 {
	if action != nil && action.WorkspaceID != nil {
		return action.WorkspaceID
	}
	if project != nil {
		return project.WorkspaceID
	}
	return nil
}

func itemMatchesTrackAndSphere(item store.Item, sphere, track string) bool {
	return item.Sphere == sphere && strings.EqualFold(strings.TrimSpace(item.Track), strings.TrimSpace(track))
}

func fallbackSphere(raw, fallback string) string {
	if clean := normalizeRuntimeActiveSphere(raw); clean != "" {
		return clean
	}
	return fallback
}

func readBrainGTDFocus(ctx context.Context, sphere string) (brainGTDFocus, error) {
	return writeBrainGTDFocus(ctx, sphere, nil)
}

func writeBrainGTDFocus(ctx context.Context, sphere string, updates map[string]interface{}) (brainGTDFocus, error) {
	return brainGTDFocusCall(ctx, sphere, updates)
}

func defaultBrainGTDFocusCall(ctx context.Context, sphere string, updates map[string]interface{}) (brainGTDFocus, error) {
	args := map[string]interface{}{"sphere": sphere}
	for key, value := range updates {
		args[key] = value
	}
	result, err := sloptoolsBrainGTDCall(nil, ctx, "brain.gtd.focus", args)
	if err != nil {
		return brainGTDFocus{}, err
	}
	var out brainGTDFocusResponse
	if err := decodeBrainGTDToolResult(result, &out); err != nil {
		return brainGTDFocus{}, err
	}
	return out.Focus, nil
}

func (a *App) itemFocusRef(itemID int64) (brainGTDFocusRef, error) {
	if itemID <= 0 {
		return brainGTDFocusRef{}, errors.New("item_id must be a positive integer")
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		return brainGTDFocusRef{}, err
	}
	return focusRefFromItem(item)
}

func (a *App) focusRefItem(ref brainGTDFocusRef) (*store.ItemSummary, error) {
	source := strings.TrimSpace(ref.Source)
	sourceRef := strings.TrimSpace(ref.Ref)
	if sourceRef == "" {
		sourceRef = strings.TrimSpace(ref.Path)
	}
	if source == "" && sourceRef != "" {
		source = store.ExternalProviderMarkdown
	}
	if source == "" || sourceRef == "" {
		return nil, errors.New("focus source reference is empty")
	}
	item, err := a.store.GetItemBySource(source, sourceRef)
	if err != nil {
		return nil, err
	}
	summary, err := a.store.GetItemSummary(item.ID)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func focusRefFromItem(item store.Item) (brainGTDFocusRef, error) {
	source := strings.TrimSpace(optionalStoreString(item.Source))
	ref := strings.TrimSpace(optionalStoreString(item.SourceRef))
	if source == "" || ref == "" {
		return brainGTDFocusRef{}, errors.New("active focus item must come from canonical GTD markdown or another sloptools-visible source")
	}
	out := brainGTDFocusRef{Source: source, Ref: ref}
	if source == store.ExternalProviderMarkdown {
		out.Path = ref
	}
	return out, nil
}
