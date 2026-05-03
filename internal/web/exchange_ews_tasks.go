package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/ews"
	"github.com/sloppy-org/slopshell/internal/projection"
	"github.com/sloppy-org/slopshell/internal/store"
)

func (a *App) syncExchangeEWSTaskAccount(ctx context.Context, account store.ExternalAccount) (int, error) {
	client, err := a.exchangeEWSClientForAccount(ctx, account)
	if err != nil {
		return 0, err
	}
	defer client.Close()

	tasks, err := client.GetTasks(ctx, "", 0, 250)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, task := range tasks {
		if _, err := a.upsertExchangeEWSTask(account, task); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (a *App) upsertExchangeEWSTask(account store.ExternalAccount, task ews.Task) (store.Item, error) {
	title := strings.TrimSpace(task.Subject)
	if title == "" {
		title = "Exchange Task"
	}
	source := account.Provider
	sourceRef := "task:" + strings.TrimSpace(task.ID)
	state := store.ItemStateInbox
	if task.IsComplete {
		state = store.ItemStateDone
	}
	var followUpAt *string
	if task.DueDate != nil && !task.DueDate.IsZero() {
		value := task.DueDate.UTC().Format(time.RFC3339)
		followUpAt = &value
	}
	if existing, err := a.store.GetItemBySource(source, sourceRef); err == nil {
		update := store.ItemUpdate{
			Title:      &title,
			FollowUpAt: followUpAt,
		}
		if update.Sphere == nil && existing.WorkspaceID == nil {
			sphere := account.Sphere
			update.Sphere = &sphere
		}
		if err := a.store.UpdateItem(existing.ID, update); err != nil {
			return store.Item{}, err
		}
		switch {
		case state == store.ItemStateDone && existing.State != store.ItemStateDone:
			if err := a.store.CompleteItemBySource(source, sourceRef); err != nil {
				return store.Item{}, err
			}
		case state == store.ItemStateInbox && existing.State == store.ItemStateDone:
			if err := a.store.SyncItemStateBySource(source, sourceRef, store.ItemStateInbox); err != nil {
				return store.Item{}, err
			}
		}
		item, err := a.store.GetItem(existing.ID)
		if err != nil {
			return store.Item{}, err
		}
		if err := a.syncExchangeEWSTaskArtifact(item, task); err != nil {
			return store.Item{}, err
		}
		return a.store.GetItem(existing.ID)
	}
	opts := store.ItemOptions{
		State:      state,
		Sphere:     &account.Sphere,
		FollowUpAt: followUpAt,
		Source:     &source,
		SourceRef:  &sourceRef,
	}
	item, err := a.store.CreateItem(title, opts)
	if err != nil {
		return store.Item{}, err
	}
	if err := a.syncExchangeEWSTaskArtifact(item, task); err != nil {
		return store.Item{}, err
	}
	return a.store.GetItem(item.ID)
}

func (a *App) syncExchangeEWSTaskArtifact(item store.Item, task ews.Task) error {
	metaJSON, err := exchangeEWSTaskMetaJSON(task)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(task.Subject)
	if title == "" {
		title = "Exchange Task"
	}
	if item.ArtifactID != nil {
		return a.store.UpdateArtifact(*item.ArtifactID, store.ArtifactUpdate{
			Kind:     artifactKindPointer(store.ArtifactKindExternalTask),
			Title:    &title,
			MetaJSON: &metaJSON,
		})
	}
	artifact, err := a.store.CreateArtifact(store.ArtifactKindExternalTask, nil, nil, &title, &metaJSON)
	if err != nil {
		return err
	}
	return a.store.UpdateItem(item.ID, store.ItemUpdate{ArtifactID: &artifact.ID})
}

func exchangeEWSTaskMetaJSON(task ews.Task) (string, error) {
	payload := map[string]any{
		"subject": task.Subject,
		"status":  task.Status,
	}
	if summary := projection.PreviewText(task.Body); summary != "" {
		payload["summary"] = summary
	}
	if task.StartDate != nil && !task.StartDate.IsZero() {
		payload["start"] = task.StartDate.UTC().Format(time.RFC3339)
	}
	if task.DueDate != nil && !task.DueDate.IsZero() {
		payload["due"] = task.DueDate.UTC().Format(time.RFC3339)
	}
	if task.CompleteDate != nil && !task.CompleteDate.IsZero() {
		payload["completed_at"] = task.CompleteDate.UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal exchange ews task meta: %w", err)
	}
	return string(raw), nil
}

func artifactKindPointer(kind store.ArtifactKind) *store.ArtifactKind {
	value := kind
	return &value
}
