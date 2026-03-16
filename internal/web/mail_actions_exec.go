package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type mailActionCommand struct {
	Action     string
	MessageIDs []string
	Folder     string
	Label      string
	Archive    *bool
}

type mailActionExecutionResult struct {
	Succeeded int
	Logs      []store.MailActionLog
}

func (a *App) executeMailAction(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, cmd mailActionCommand) (mailActionExecutionResult, error) {
	messageIDs := compactStringList(cmd.MessageIDs)
	if len(messageIDs) == 0 {
		return mailActionExecutionResult{}, errBadRequest("message_ids are required")
	}
	action := strings.TrimSpace(strings.ToLower(cmd.Action))
	if action == "" {
		return mailActionExecutionResult{}, errBadRequest("action is required")
	}

	resolvedMessages, _ := provider.GetMessages(ctx, messageIDs, "full")
	byID := map[string]*providerdata.EmailMessage{}
	for _, message := range resolvedMessages {
		if message == nil {
			continue
		}
		if id := strings.TrimSpace(message.ID); id != "" {
			byID[id] = message
		}
	}

	targetFolder := mailActionTargetFolder(account, action, strings.TrimSpace(cmd.Folder), strings.TrimSpace(cmd.Label))
	requestPayload := map[string]any{
		"action":      action,
		"message_ids": append([]string(nil), messageIDs...),
		"folder":      strings.TrimSpace(cmd.Folder),
		"label":       strings.TrimSpace(cmd.Label),
	}
	if cmd.Archive != nil {
		requestPayload["archive"] = *cmd.Archive
	}

	logs := make([]store.MailActionLog, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		message := byID[messageID]
		logEntry, err := a.store.CreateMailActionLog(store.MailActionLogInput{
			AccountID:  account.ID,
			Provider:   account.Provider,
			MessageID:  messageID,
			Action:     action,
			FolderFrom: mailActionMessageFolder(message),
			FolderTo:   targetFolder,
			Subject:    mailActionMessageSubject(message),
			Sender:     mailActionMessageSender(message),
			Request:    requestPayload,
			Status:     store.MailActionLogPending,
		})
		if err != nil {
			return mailActionExecutionResult{}, err
		}
		logs = append(logs, logEntry)
	}

	applied, err := applyMailAction(ctx, account, provider, action, messageIDs, strings.TrimSpace(cmd.Folder), strings.TrimSpace(cmd.Label), cmd.Archive)
	if err != nil {
		for _, logEntry := range logs {
			_ = a.store.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogFailed, "", err.Error())
		}
		return mailActionExecutionResult{Logs: logs}, err
	}

	if err := a.applyMailActionResolutions(account, action, targetFolder, applied.Resolutions); err != nil {
		for _, logEntry := range logs {
			_ = a.store.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogReconcileFailed, "", err.Error())
		}
		return mailActionExecutionResult{Succeeded: applied.Count, Logs: logs}, fmt.Errorf("mail action applied remotely but local binding update failed: %w", err)
	}

	reconcileIDs := mergeMailActionReconcileIDs(messageIDs, applied.Resolutions)
	if err := a.forceMailActionReconcile(ctx, account, reconcileIDs); err != nil {
		for _, logEntry := range logs {
			_ = a.store.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogReconcileFailed, "", err.Error())
		}
		return mailActionExecutionResult{Succeeded: applied.Count, Logs: logs}, fmt.Errorf("mail action applied remotely but reconcile failed: %w", err)
	}

	resolvedByMessageID := make(map[string]string, len(applied.Resolutions))
	for _, resolution := range applied.Resolutions {
		resolvedByMessageID[strings.TrimSpace(resolution.OriginalMessageID)] = strings.TrimSpace(resolution.ResolvedMessageID)
	}
	for _, logEntry := range logs {
		resolvedID := resolvedByMessageID[strings.TrimSpace(logEntry.MessageID)]
		_ = a.store.UpdateMailActionLogResult(logEntry.ID, store.MailActionLogApplied, resolvedID, "")
	}
	return mailActionExecutionResult{Succeeded: applied.Count, Logs: logs}, nil
}

func (a *App) forceMailActionReconcile(ctx context.Context, account store.ExternalAccount, messageIDs []string) error {
	if a != nil && a.emailRefreshes != nil {
		a.emailRefreshes.add(account.ID, messageIDs...)
	}
	if a != nil && a.syncMailAccountNow != nil {
		_, err := a.syncMailAccountNow(ctx, account)
		return err
	}
	if a == nil {
		return nil
	}
	_, err := a.syncEmailAccount(ctx, account)
	return err
}

func (a *App) applyMailActionResolutions(account store.ExternalAccount, action, targetFolder string, resolutions []email.ActionResolution) error {
	if a == nil || a.store == nil || len(resolutions) == 0 {
		return nil
	}
	var (
		containerRef *string
		itemState    *string
	)
	if strings.TrimSpace(targetFolder) != "" {
		containerRef = &targetFolder
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "move_to_inbox":
		state := store.ItemStateInbox
		itemState = &state
	case "archive", "archive_label", "trash", "delete", "move_to_folder":
		state := store.ItemStateDone
		itemState = &state
	}
	updates := make([]store.ExternalBindingReconcileUpdate, 0, len(resolutions))
	for _, resolution := range resolutions {
		updates = append(updates, store.ExternalBindingReconcileUpdate{
			ObjectType:        emailBindingObjectType,
			OldRemoteID:       resolution.OriginalMessageID,
			NewRemoteID:       resolution.ResolvedMessageID,
			ContainerRef:      containerRef,
			FollowUpItemState: itemState,
		})
	}
	return a.store.ApplyExternalBindingReconcileUpdates(account.ID, account.Provider, updates)
}

func mergeMailActionReconcileIDs(messageIDs []string, resolutions []email.ActionResolution) []string {
	ids := make(map[string]struct{}, len(messageIDs)+len(resolutions))
	for _, id := range messageIDs {
		if clean := strings.TrimSpace(id); clean != "" {
			ids[clean] = struct{}{}
		}
	}
	for _, resolution := range resolutions {
		if clean := strings.TrimSpace(resolution.OriginalMessageID); clean != "" {
			ids[clean] = struct{}{}
		}
		if clean := strings.TrimSpace(resolution.ResolvedMessageID); clean != "" {
			ids[clean] = struct{}{}
		}
	}
	return sortedEmailMessageIDs(ids)
}

func mailActionTargetFolder(account store.ExternalAccount, action, folder, label string) string {
	switch action {
	case "move_to_inbox":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Posteingang"
		}
		return "inbox"
	case "trash":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Gelöschte Elemente"
		}
		return "trash"
	case "archive":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Archive"
		}
		return "archive"
	case "move_to_folder":
		return folder
	case "archive_label":
		if account.Provider == store.ExternalProviderExchangeEWS {
			return "Archive/" + label
		}
		return label
	case "apply_label":
		return label
	default:
		return ""
	}
}

func mailActionMessageFolder(message *providerdata.EmailMessage) string {
	if message == nil || len(message.Labels) == 0 {
		return ""
	}
	return strings.Join(message.Labels, ",")
}

func mailActionMessageSubject(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	return strings.TrimSpace(message.Subject)
}

func mailActionMessageSender(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	return strings.TrimSpace(message.Sender)
}
