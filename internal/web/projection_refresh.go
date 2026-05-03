package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
	tabsync "github.com/sloppy-org/slopshell/internal/sync"
)

type affectedRef struct {
	Domain      string `json:"domain"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider,omitempty"`
	AccountID   int64  `json:"account_id,omitempty"`
	ID          string `json:"id,omitempty"`
	PreviousID  string `json:"previous_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Sphere      string `json:"sphere,omitempty"`
}

func (a *App) refreshAffectedResult(ctx context.Context, endpoint mcpEndpoint, toolName string, result map[string]any) (int, error) {
	if a == nil || a.store == nil {
		return 0, nil
	}
	refs := affectedRefsFromResult(result)
	if len(refs) == 0 {
		return 0, nil
	}
	endpoint = projectionRefreshEndpoint(a, endpoint)
	normalizedTool := normalizeAffectedToolName(toolName)
	refreshed := make([]affectedRef, 0, len(refs))
	mailGroups := map[int64][]affectedRef{}
	for _, ref := range refs {
		if ref.Domain == "mail" && ref.Kind == "message" {
			mailGroups[ref.AccountID] = append(mailGroups[ref.AccountID], ref)
			continue
		}
		changed, err := a.refreshAffectedRef(ctx, endpoint, normalizedTool, result, ref)
		if err != nil {
			return len(refreshed), err
		}
		if changed {
			refreshed = append(refreshed, ref)
		}
	}
	for _, group := range mailGroups {
		changed, err := a.refreshAffectedMailMessages(ctx, group)
		if err != nil {
			return len(refreshed), err
		}
		if changed {
			refreshed = append(refreshed, group...)
		}
	}
	a.broadcastProjectionRowsChanged(refreshed)
	return len(refreshed), nil
}

func (a *App) finalizeLocalAssistantMCPResult(ctx context.Context, endpoint mcpEndpoint, toolName string, result localAssistantToolResult) (localAssistantToolResult, error) {
	if _, err := a.refreshAffectedResult(ctx, endpoint, toolName, result.StructuredContent); err != nil {
		result.IsError = true
		result.Error = "tool succeeded but local projection refresh failed: " + err.Error()
	}
	return result, nil
}

func projectionRefreshEndpoint(a *App, endpoint mcpEndpoint) mcpEndpoint {
	if endpoint.ok() {
		return endpoint
	}
	if a != nil {
		return a.localControlEndpoint
	}
	return endpoint
}

func normalizeAffectedToolName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), ".", "_")
}

func affectedRefsFromResult(result map[string]any) []affectedRef {
	if result == nil {
		return nil
	}
	return compactAffectedRefs(affectedRefsFromAny(result["affected"])...)
}

func affectedRefsFromAny(raw any) []affectedRef {
	switch typed := raw.(type) {
	case []affectedRef:
		return compactAffectedRefs(typed...)
	case []map[string]any:
		out := make([]affectedRef, 0, len(typed))
		for _, item := range typed {
			out = append(out, affectedRefFromMap(item))
		}
		return compactAffectedRefs(out...)
	case []any:
		out := make([]affectedRef, 0, len(typed))
		for _, item := range typed {
			obj, _ := item.(map[string]any)
			if obj == nil {
				continue
			}
			out = append(out, affectedRefFromMap(obj))
		}
		return compactAffectedRefs(out...)
	default:
		return nil
	}
}

func affectedRefFromMap(obj map[string]any) affectedRef {
	return normalizeAffectedRef(affectedRef{
		Domain:      strings.TrimSpace(fmt.Sprint(obj["domain"])),
		Kind:        strings.TrimSpace(fmt.Sprint(obj["kind"])),
		Provider:    strings.TrimSpace(fmt.Sprint(obj["provider"])),
		AccountID:   int64(intFromAny(obj["account_id"], 0)),
		ID:          strings.TrimSpace(fmt.Sprint(obj["id"])),
		PreviousID:  strings.TrimSpace(fmt.Sprint(obj["previous_id"])),
		ContainerID: strings.TrimSpace(fmt.Sprint(obj["container_id"])),
		Path:        strings.TrimSpace(fmt.Sprint(obj["path"])),
		Sphere:      strings.TrimSpace(fmt.Sprint(obj["sphere"])),
	})
}

func normalizeAffectedRef(ref affectedRef) affectedRef {
	ref.Domain = strings.ToLower(strings.TrimSpace(ref.Domain))
	ref.Kind = strings.ToLower(strings.TrimSpace(ref.Kind))
	ref.Provider = strings.ToLower(strings.TrimSpace(ref.Provider))
	ref.ID = strings.TrimSpace(ref.ID)
	ref.PreviousID = strings.TrimSpace(ref.PreviousID)
	ref.ContainerID = strings.TrimSpace(ref.ContainerID)
	ref.Path = strings.TrimSpace(ref.Path)
	ref.Sphere = strings.TrimSpace(ref.Sphere)
	return ref
}

func compactAffectedRefs(refs ...affectedRef) []affectedRef {
	out := make([]affectedRef, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = normalizeAffectedRef(ref)
		if ref.Kind == "" {
			continue
		}
		if ref.ID == "" && ref.Path == "" {
			continue
		}
		key := strings.Join([]string{
			ref.Domain,
			ref.Kind,
			ref.Provider,
			ref.Sphere,
			ref.Path,
			ref.ContainerID,
			ref.PreviousID,
			ref.ID,
			fmt.Sprintf("%d", ref.AccountID),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (a *App) refreshAffectedRef(ctx context.Context, endpoint mcpEndpoint, toolName string, result map[string]any, ref affectedRef) (bool, error) {
	switch {
	case ref.Domain == "brain" && ref.Kind == "gtd_commitment":
		return a.refreshAffectedBrainCommitment(ctx, endpoint, ref)
	case ref.Domain == "tasks" && ref.Kind == "task":
		return a.refreshAffectedTask(ctx, endpoint, toolName, result, ref)
	default:
		return false, nil
	}
}

func (a *App) refreshAffectedMailMessages(ctx context.Context, refs []affectedRef) (bool, error) {
	if a == nil || a.store == nil || len(refs) == 0 {
		return false, nil
	}
	accountID := refs[0].AccountID
	if accountID <= 0 {
		return false, nil
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	ids := make([]string, 0, len(refs)*2)
	for _, ref := range refs {
		ids = append(ids, ref.ID, ref.PreviousID)
	}
	ids = compactStringList(ids)
	if len(ids) == 0 {
		return false, nil
	}
	if err := a.forceMailActionReconcile(ctx, account, ids); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) refreshAffectedBrainCommitment(ctx context.Context, endpoint mcpEndpoint, ref affectedRef) (bool, error) {
	if a == nil || a.store == nil {
		return false, nil
	}
	path := strings.TrimSpace(firstNonEmptyString(ref.Path, ref.ID))
	if path == "" {
		return false, nil
	}
	sphere := strings.TrimSpace(ref.Sphere)
	if sphere == "" {
		sphere = store.SphereWork
	}
	endpoint, err := sloptoolsEndpointForApp(a)
	if err != nil {
		return false, err
	}
	if !endpoint.ok() {
		return false, errors.New("sloptools MCP endpoint is not configured")
	}
	parsed, err := mcpToolsCallEndpoint(endpoint, gtdParseTool, map[string]any{
		"sphere": sphere,
		"path":   path,
	})
	if err != nil {
		return false, err
	}
	review, canonical, err := brainGTDAffectedItemsFromParseResult(parsed, sphere, path)
	if err != nil {
		return false, err
	}
	bindingResult, err := a.syncBrainGTDCanonicalBindings(ctx, sphere, brainGTDCommitmentList{
		Items: []brainGTDCommitmentItem{canonical},
		Count: 1,
	})
	if err != nil {
		return false, err
	}
	changed, err := a.upsertBrainGTDReviewItem(ctx, sphere, review)
	if err != nil {
		return false, err
	}
	return changed || bindingResult.Migrated > 0 || bindingResult.Merged > 0, nil
}

func brainGTDAffectedItemsFromParseResult(result map[string]any, sphere, path string) (brainGTDReviewItem, brainGTDCommitmentItem, error) {
	commitment, _ := result["commitment"].(map[string]any)
	if commitment == nil {
		return brainGTDReviewItem{}, brainGTDCommitmentItem{}, errors.New("brain.note.parse returned no commitment")
	}
	title := strings.TrimSpace(firstNonEmptyString(mapString(commitment, "title"), path))
	project := strings.TrimSpace(mapString(commitment, "project"))
	track := strings.TrimSpace(firstNonEmptyString(trackFromLabels(anyStringSlice(commitment["labels"])), mapString(commitment, "track")))
	status := strings.TrimSpace(firstNonEmptyString(commitmentOverlayString(commitment, "status"), mapString(commitment, "status")))
	followUp := strings.TrimSpace(firstNonEmptyString(commitmentOverlayString(commitment, "follow_up"), mapString(commitment, "follow_up")))
	due := strings.TrimSpace(firstNonEmptyString(commitmentOverlayString(commitment, "due"), mapString(commitment, "due")))
	bindings := sourceBindingRefs(commitment["source_bindings"])
	review := brainGTDReviewItem{
		ID:        store.ExternalProviderMarkdown + ":" + path,
		Source:    store.ExternalProviderMarkdown,
		SourceRef: path,
		Title:     title,
		Status:    status,
		Queue:     status,
		Kind:      "commitment",
		Path:      path,
		Project:   project,
		Track:     track,
		Due:       due,
		FollowUp:  followUp,
	}
	canonical := brainGTDCommitmentItem{
		Path:     path,
		Title:    title,
		Status:   status,
		Project:  project,
		Track:    track,
		Due:      due,
		FollowUp: followUp,
		Bindings: bindings,
	}
	if strings.TrimSpace(sphere) == "" {
		sphere = store.SphereWork
	}
	return review, canonical, nil
}

func commitmentOverlayString(commitment map[string]any, field string) string {
	overlay, _ := commitment["local_overlay"].(map[string]any)
	return mapString(overlay, field)
}

func mapString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(obj[key]))
}

func anyStringSlice(raw any) []string {
	values, _ := raw.([]any)
	if len(values) == 0 {
		if typed, ok := raw.([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(fmt.Sprint(value))
		if clean == "" || clean == "<nil>" {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func trackFromLabels(labels []string) string {
	for _, label := range labels {
		clean := strings.TrimSpace(label)
		if strings.HasPrefix(clean, "track/") {
			return strings.TrimPrefix(clean, "track/")
		}
	}
	return ""
}

func sourceBindingRefs(raw any) []string {
	bindings, _ := raw.([]any)
	if len(bindings) == 0 {
		return nil
	}
	out := make([]string, 0, len(bindings))
	for _, value := range bindings {
		binding, _ := value.(map[string]any)
		provider := strings.TrimSpace(fmt.Sprint(binding["provider"]))
		ref := strings.TrimSpace(fmt.Sprint(binding["ref"]))
		if provider == "" || ref == "" {
			continue
		}
		out = append(out, provider+":"+ref)
	}
	return out
}

func (a *App) refreshAffectedTask(ctx context.Context, endpoint mcpEndpoint, toolName string, result map[string]any, ref affectedRef) (bool, error) {
	if a == nil || a.store == nil {
		return false, nil
	}
	if ref.AccountID <= 0 || ref.ID == "" {
		return false, nil
	}
	if affectedTaskDeleted(toolName, result) {
		return a.deleteAffectedTask(ref)
	}
	if !endpoint.ok() {
		return false, errors.New("MCP endpoint is not configured")
	}
	taskResult, err := mcpToolsCallEndpoint(endpoint, "task_get", map[string]any{
		"account_id": ref.AccountID,
		"list_id":    ref.ContainerID,
		"id":         ref.ID,
	})
	if err != nil {
		return false, err
	}
	task, _ := taskResult["task"].(map[string]any)
	if task == nil {
		return false, errors.New("task_get returned no task payload")
	}
	return a.upsertAffectedTask(ctx, ref, task)
}

func affectedTaskDeleted(toolName string, result map[string]any) bool {
	if normalizeAffectedToolName(toolName) == "task_delete" {
		return true
	}
	deleted, ok := parseOptionalBool(result["deleted"])
	return ok && deleted
}

func (a *App) deleteAffectedTask(ref affectedRef) (bool, error) {
	if a == nil || a.store == nil || ref.AccountID <= 0 || ref.ID == "" {
		return false, nil
	}
	binding, err := a.store.GetBindingByRemote(ref.AccountID, ref.Provider, "task", ref.ID)
	if err == nil && binding.ItemID != nil {
		return true, a.store.DeleteItem(*binding.ItemID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for _, sourceRef := range affectedTaskSourceRefCandidates(ref, nil) {
		item, itemErr := a.store.GetItemBySource(ref.Provider, sourceRef)
		if itemErr == nil {
			return true, a.store.DeleteItem(item.ID)
		}
		if !errors.Is(itemErr, sql.ErrNoRows) {
			return false, itemErr
		}
	}
	return false, nil
}

func (a *App) upsertAffectedTask(ctx context.Context, ref affectedRef, task map[string]any) (bool, error) {
	account, err := a.store.GetExternalAccount(ref.AccountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	sourceRef, existing, err := a.affectedTaskSourceRef(ref, task)
	if err != nil {
		return false, err
	}
	if sourceRef == "" {
		return false, nil
	}
	title := strings.TrimSpace(firstNonEmptyString(mapString(task, "title"), "Task"))
	metaJSON, err := json.Marshal(task)
	if err != nil {
		return false, err
	}
	binding := store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "task",
		RemoteID:     ref.ID,
		ContainerRef: optionalString(strings.TrimSpace(firstNonEmptyString(mapString(task, "project_id"), ref.ContainerID))),
	}
	sink := tabsync.NewStoreSink(a.store)
	artifact, err := sink.UpsertArtifact(ctx, store.Artifact{
		Kind:     store.ArtifactKindExternalTask,
		Title:    optionalString(title),
		MetaJSON: stringPointer(string(metaJSON)),
	}, binding)
	if err != nil {
		return false, err
	}
	provider := account.Provider
	incoming := store.Item{
		Title:        title,
		Kind:         store.ItemKindAction,
		State:        affectedTaskState(existing, task),
		Sphere:       account.Sphere,
		ArtifactID:   &artifact.ID,
		VisibleAfter: affectedTaskTime(task, "start_at"),
		FollowUpAt:   affectedTaskTime(task, "start_at"),
		DueAt:        affectedTaskTime(task, "due"),
		Source:       &provider,
		SourceRef:    &sourceRef,
	}
	updated, err := sink.UpsertItemFromSource(ctx, incoming, binding)
	if err != nil {
		return false, err
	}
	return existing == nil || genericTaskChanged(*existing, updated), nil
}

func (a *App) affectedTaskSourceRef(ref affectedRef, task map[string]any) (string, *store.Item, error) {
	if a == nil || a.store == nil {
		return "", nil, nil
	}
	binding, err := a.store.GetBindingByRemote(ref.AccountID, ref.Provider, "task", ref.ID)
	if err == nil && binding.ItemID != nil {
		item, itemErr := a.store.GetItem(*binding.ItemID)
		if itemErr == nil {
			sourceRef := strings.TrimSpace(optionalStoreString(item.SourceRef))
			if sourceRef == "" {
				sourceRef = defaultAffectedTaskSourceRef(ref, task)
			}
			return sourceRef, &item, nil
		}
		if !errors.Is(itemErr, sql.ErrNoRows) {
			return "", nil, itemErr
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", nil, err
	}
	for _, candidate := range affectedTaskSourceRefCandidates(ref, task) {
		item, itemErr := a.store.GetItemBySource(ref.Provider, candidate)
		if itemErr == nil {
			return candidate, &item, nil
		}
		if !errors.Is(itemErr, sql.ErrNoRows) {
			return "", nil, itemErr
		}
	}
	return defaultAffectedTaskSourceRef(ref, task), nil, nil
}

func affectedTaskSourceRefCandidates(ref affectedRef, task map[string]any) []string {
	candidates := []string{
		defaultAffectedTaskSourceRef(ref, task),
		"task:" + strings.TrimSpace(ref.ID),
		strings.TrimSpace(ref.ID),
	}
	if ref.ContainerID != "" && ref.ID != "" {
		candidates = append(candidates, ref.ContainerID+"/"+ref.ID)
	}
	providerRef := strings.TrimSpace(mapString(task, "provider_ref"))
	if providerRef != "" {
		candidates = append(candidates, providerRef)
	}
	return compactStringList(candidates)
}

func defaultAffectedTaskSourceRef(ref affectedRef, task map[string]any) string {
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		id = strings.TrimSpace(mapString(task, "id"))
	}
	if id == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(ref.Provider)) {
	case store.ExternalProviderTodoist, store.ExternalProviderExchangeEWS:
		return "task:" + id
	case store.ExternalProviderGoogleTasks:
		if ref.ContainerID != "" {
			return ref.ContainerID + "/" + id
		}
	}
	if providerRef := strings.TrimSpace(mapString(task, "provider_ref")); providerRef != "" {
		return providerRef
	}
	return id
}

func affectedTaskState(existing *store.Item, task map[string]any) string {
	if completed, ok := parseOptionalBool(task["completed"]); ok && completed {
		return store.ItemStateDone
	}
	if startAt := affectedTaskTime(task, "start_at"); startAt != nil {
		if parsed, err := time.Parse(time.RFC3339, *startAt); err == nil && parsed.After(time.Now().UTC()) {
			if existing != nil && existing.State == store.ItemStateWaiting {
				return existing.State
			}
			return store.ItemStateDeferred
		}
	}
	if existing != nil && existing.State != "" && existing.State != store.ItemStateDone {
		return existing.State
	}
	return store.ItemStateNext
}

func affectedTaskTime(task map[string]any, field string) *string {
	value := strings.TrimSpace(mapString(task, field))
	if value == "" {
		return nil
	}
	return &value
}

func genericTaskChanged(existing, incoming store.Item) bool {
	return existing.Title != incoming.Title ||
		existing.State != incoming.State ||
		existing.Kind != incoming.Kind ||
		optionalStoreString(existing.VisibleAfter) != optionalStoreString(incoming.VisibleAfter) ||
		optionalStoreString(existing.FollowUpAt) != optionalStoreString(incoming.FollowUpAt) ||
		optionalStoreString(existing.DueAt) != optionalStoreString(incoming.DueAt) ||
		optionalStoreString(existing.SourceRef) != optionalStoreString(incoming.SourceRef) ||
		existing.Track != incoming.Track
}

func (a *App) broadcastProjectionRowsChanged(refs []affectedRef) {
	if a == nil || len(refs) == 0 {
		return
	}
	domains := make([]string, 0, len(refs))
	kinds := make([]string, 0, len(refs))
	seenDomains := map[string]struct{}{}
	seenKinds := map[string]struct{}{}
	for _, ref := range refs {
		if ref.Domain != "" {
			if _, ok := seenDomains[ref.Domain]; !ok {
				seenDomains[ref.Domain] = struct{}{}
				domains = append(domains, ref.Domain)
			}
		}
		if ref.Kind != "" {
			if _, ok := seenKinds[ref.Kind]; !ok {
				seenKinds[ref.Kind] = struct{}{}
				kinds = append(kinds, ref.Kind)
			}
		}
	}
	encoded, err := json.Marshal(map[string]any{
		"type":     "projection_rows_changed",
		"count":    len(refs),
		"domains":  domains,
		"kinds":    kinds,
		"affected": refs,
	})
	if err != nil {
		return
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeText(encoded)
	})
}
