package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/krystophny/tabura/internal/ews"
	"github.com/krystophny/tabura/internal/providerdata"
)

type ExchangeEWSConfig struct {
	Label         string
	Endpoint      string
	Username      string
	Password      string
	ServerVersion string
	BatchSize     int
	InsecureTLS   bool
	ArchiveFolder string
}

type ExchangeEWSMailProvider struct {
	client *ews.Client
	cfg    ExchangeEWSConfig
}

var _ EmailProvider = (*ExchangeEWSMailProvider)(nil)
var _ DraftProvider = (*ExchangeEWSMailProvider)(nil)

func ExchangeEWSConfigFromMap(label string, config map[string]any) (ExchangeEWSConfig, error) {
	cfg := ExchangeEWSConfig{Label: strings.TrimSpace(label)}
	if raw, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = strings.TrimSpace(raw)
	}
	if raw, ok := config["base_url"].(string); ok && cfg.Endpoint == "" {
		cfg.Endpoint = strings.TrimSpace(raw)
	}
	if raw, ok := config["username"].(string); ok {
		cfg.Username = strings.TrimSpace(raw)
	}
	if raw, ok := config["server_version"].(string); ok {
		cfg.ServerVersion = strings.TrimSpace(raw)
	}
	if raw, ok := config["archive_folder"].(string); ok {
		cfg.ArchiveFolder = strings.TrimSpace(raw)
	}
	if raw, ok := config["batch_size"].(float64); ok {
		cfg.BatchSize = int(raw)
	}
	if raw, ok := config["batch_size"].(int); ok {
		cfg.BatchSize = raw
	}
	if raw, ok := config["insecure_tls"].(bool); ok {
		cfg.InsecureTLS = raw
	}
	return cfg, nil
}

func NewExchangeEWSMailProvider(cfg ExchangeEWSConfig) (*ExchangeEWSMailProvider, error) {
	client, err := ews.NewClient(ews.Config{
		Endpoint:      cfg.Endpoint,
		Username:      cfg.Username,
		Password:      cfg.Password,
		ServerVersion: cfg.ServerVersion,
		BatchSize:     cfg.BatchSize,
		InsecureTLS:   cfg.InsecureTLS,
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ArchiveFolder) == "" {
		cfg.ArchiveFolder = "Archive"
	}
	return &ExchangeEWSMailProvider{client: client, cfg: cfg}, nil
}

func (p *ExchangeEWSMailProvider) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]providerdata.Label, 0, len(folders))
	for _, folder := range folders {
		if folder.Kind == ews.FolderKindCalendar || folder.Kind == ews.FolderKindContacts || folder.Kind == ews.FolderKindTasks {
			continue
		}
		name := exchangeEWSDisplayFolderName(folder.Name)
		if name == "" {
			continue
		}
		out = append(out, providerdata.Label{
			ID:             folder.ID,
			Name:           name,
			Type:           "exchange_ews",
			MessagesTotal:  folder.TotalCount,
			MessagesUnread: folder.UnreadCount,
		})
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("exchange ews provider is not configured")
	}
	candidates, err := p.searchFolders(ctx, opts)
	if err != nil {
		return nil, err
	}
	maxResults := int(opts.MaxResults)
	if maxResults <= 0 {
		maxResults = 100
	}
	found := make(map[string]struct{}, maxResults)
	for _, folder := range candidates {
		page, err := p.client.FindMessages(ctx, folder, 0, maxResults)
		if err != nil {
			return nil, err
		}
		if len(page.ItemIDs) == 0 {
			continue
		}
		messages, err := p.client.GetMessages(ctx, page.ItemIDs)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if !matchExchangeEWSMessage(message, opts) {
				continue
			}
			found[message.ID] = struct{}{}
			if len(found) >= maxResults {
				return sortedMessageIDs(found), nil
			}
		}
	}
	return sortedMessageIDs(found), nil
}

func (p *ExchangeEWSMailProvider) GetMessage(ctx context.Context, messageID, _ string) (*providerdata.EmailMessage, error) {
	messages, err := p.client.GetMessages(ctx, []string{messageID})
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("exchange ews message %q not found", strings.TrimSpace(messageID))
	}
	decoded := decodeExchangeEWSMessage(messages[0])
	return &decoded, nil
}

func (p *ExchangeEWSMailProvider) GetMessages(ctx context.Context, messageIDs []string, _ string) ([]*providerdata.EmailMessage, error) {
	messages, err := p.client.GetMessages(ctx, messageIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*providerdata.EmailMessage, 0, len(messages))
	for _, message := range messages {
		decoded := decodeExchangeEWSMessage(message)
		out = append(out, &decoded)
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetReadState(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.SetReadState(ctx, ids, false); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) Archive(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	folderID, err := p.resolveArchiveFolderID(ctx)
	if err != nil {
		return 0, err
	}
	if folderID == "" {
		return 0, fmt.Errorf("exchange ews archive folder is not configured")
	}
	if err := p.client.MoveItems(ctx, ids, folderID); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) MoveToInbox(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.MoveItems(ctx, ids, "inbox"); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) Trash(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.DeleteItems(ctx, ids, false); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) Delete(ctx context.Context, messageIDs []string) (int, error) {
	ids := compactMessageIDs(messageIDs)
	if err := p.client.DeleteItems(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (p *ExchangeEWSMailProvider) ProviderName() string { return "exchange_ews" }

func (p *ExchangeEWSMailProvider) CreateDraft(ctx context.Context, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := p.normalizeDraftInput(input, false)
	if err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	created, err := p.client.CreateDraft(ctx, ews.DraftMessage{
		Subject:    normalized.Subject,
		MIME:       raw,
		ThreadID:   normalized.ThreadID,
		InReplyTo:  normalized.InReplyTo,
		References: normalized.References,
	})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews create draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(created.ID), ThreadID: strings.TrimSpace(created.ConversationID)}, nil
}

func (p *ExchangeEWSMailProvider) CreateReplyDraft(ctx context.Context, messageID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return Draft{}, fmt.Errorf("exchange ews message id is required")
	}
	message, err := p.client.GetMessages(ctx, []string{messageID})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews get reply message: %w", err)
	}
	if len(message) == 0 {
		return Draft{}, fmt.Errorf("exchange ews message %q not found", messageID)
	}
	seed := input
	remote := message[0]
	if len(seed.To) == 0 {
		addr := strings.TrimSpace(remote.From.Email)
		if addr != "" {
			if strings.TrimSpace(remote.From.Name) != "" {
				addr = (&mail.Address{Name: strings.TrimSpace(remote.From.Name), Address: addr}).String()
			}
			seed.To = []string{addr}
		}
	}
	if strings.TrimSpace(seed.Subject) == "" {
		seed.Subject = ensureReplySubject(remote.Subject)
	} else {
		seed.Subject = ensureReplySubject(seed.Subject)
	}
	if strings.TrimSpace(seed.ThreadID) == "" {
		seed.ThreadID = strings.TrimSpace(remote.ConversationID)
	}
	if strings.TrimSpace(seed.InReplyTo) == "" {
		seed.InReplyTo = strings.TrimSpace(remote.InternetMessageID)
	}
	if len(seed.References) == 0 && strings.TrimSpace(remote.InternetMessageID) != "" {
		seed.References = []string{strings.TrimSpace(remote.InternetMessageID)}
	}
	return p.CreateDraft(ctx, seed)
}

func (p *ExchangeEWSMailProvider) UpdateDraft(ctx context.Context, draftID string, input DraftInput) (Draft, error) {
	if p == nil || p.client == nil {
		return Draft{}, fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := p.normalizeDraftInput(input, false)
	if err != nil {
		return Draft{}, err
	}
	raw, err := buildRFC822Message(normalized)
	if err != nil {
		return Draft{}, err
	}
	updated, err := p.client.UpdateDraft(ctx, strings.TrimSpace(draftID), ews.DraftMessage{
		Subject:    normalized.Subject,
		MIME:       raw,
		ThreadID:   normalized.ThreadID,
		InReplyTo:  normalized.InReplyTo,
		References: normalized.References,
	})
	if err != nil {
		return Draft{}, fmt.Errorf("exchange ews update draft: %w", err)
	}
	return Draft{ID: strings.TrimSpace(updated.ID), ThreadID: strings.TrimSpace(updated.ConversationID)}, nil
}

func (p *ExchangeEWSMailProvider) SendDraft(ctx context.Context, draftID string, input DraftInput) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("exchange ews provider is not configured")
	}
	normalized, err := NormalizeDraftSendInput(input)
	if err != nil {
		return err
	}
	if strings.TrimSpace(draftID) == "" {
		created, err := p.CreateDraft(ctx, normalized)
		if err != nil {
			return err
		}
		draftID = created.ID
	}
	if _, err := p.UpdateDraft(ctx, strings.TrimSpace(draftID), normalized); err != nil {
		return err
	}
	if err := p.client.SendDraft(ctx, strings.TrimSpace(draftID)); err != nil {
		return fmt.Errorf("exchange ews send draft: %w", err)
	}
	return nil
}

func (p *ExchangeEWSMailProvider) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

func (p *ExchangeEWSMailProvider) searchFolders(ctx context.Context, opts SearchOptions) ([]string, error) {
	if strings.TrimSpace(opts.Folder) != "" {
		ref, err := p.resolveFolderRef(ctx, strings.TrimSpace(opts.Folder))
		if err != nil {
			return nil, err
		}
		return []string{ref}, nil
	}
	folders, err := p.client.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder.Kind == ews.FolderKindCalendar || folder.Kind == ews.FolderKindContacts || folder.Kind == ews.FolderKindTasks {
			continue
		}
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			continue
		}
		if !opts.IncludeSpamTrash && exchangeEWSSpamOrTrash(name) {
			continue
		}
		if strings.TrimSpace(folder.ID) != "" {
			out = append(out, strings.TrimSpace(folder.ID))
		}
	}
	if len(out) == 0 {
		out = append(out, "inbox")
	}
	return out, nil
}

func (p *ExchangeEWSMailProvider) resolveFolderRef(ctx context.Context, folder string) (string, error) {
	clean := strings.TrimSpace(folder)
	if clean == "" {
		return "inbox", nil
	}
	switch strings.ToLower(clean) {
	case "inbox", "posteingang":
		return "inbox", nil
	case "drafts", "entwürfe", "entwuerfe":
		return "drafts", nil
	case "sent", "sentitems", "gesendete elemente":
		return "sentitems", nil
	case "trash", "deleteditems", "gelöschte elemente", "geloschte elemente":
		return "deleteditems", nil
	case "junk", "spam", "junkemail", "junk-e-mail":
		return "junkemail", nil
	case "archive":
		return p.resolveArchiveFolderID(ctx)
	}
	folderInfo, err := p.client.FindFolderByName(ctx, clean)
	if err != nil {
		return "", err
	}
	if folderInfo != nil && strings.TrimSpace(folderInfo.ID) != "" {
		return strings.TrimSpace(folderInfo.ID), nil
	}
	return clean, nil
}

func (p *ExchangeEWSMailProvider) normalizeDraftInput(input DraftInput, requireRecipients bool) (DraftInput, error) {
	reply := input
	if strings.TrimSpace(reply.From) == "" {
		reply.From = strings.TrimSpace(p.cfg.Username)
	}
	if requireRecipients {
		return NormalizeDraftSendInput(reply)
	}
	return NormalizeDraftInput(reply)
}

func (p *ExchangeEWSMailProvider) resolveArchiveFolderID(ctx context.Context) (string, error) {
	folder, err := p.client.FindFolderByName(ctx, p.cfg.ArchiveFolder)
	if err != nil || folder == nil {
		return "", err
	}
	return folder.ID, nil
}

func compactMessageIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func sortedMessageIDs(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func exchangeEWSSpamOrTrash(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "junk-e-mail", "junk email", "spam", "geloschte elemente", "gelöschte elemente", "deleted items":
		return true
	default:
		return false
	}
}

func exchangeEWSDisplayFolderName(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	if strings.EqualFold(clean, "Archive") {
		return ""
	}
	clean = strings.TrimPrefix(clean, "Archive/")
	clean = strings.TrimPrefix(clean, "Archive\\")
	if idx := strings.LastIndexAny(clean, `/\`); idx >= 0 {
		clean = clean[idx+1:]
	}
	return strings.TrimSpace(clean)
}

func matchExchangeEWSMessage(message ews.Message, opts SearchOptions) bool {
	if opts.IsRead != nil && message.IsRead != *opts.IsRead {
		return false
	}
	if opts.HasAttachment != nil && message.HasAttachments != *opts.HasAttachment {
		return false
	}
	if opts.IsFlagged != nil {
		flagged := strings.EqualFold(strings.TrimSpace(message.FlagStatus), "Flagged")
		if flagged != *opts.IsFlagged {
			return false
		}
	}
	haystack := strings.ToLower(strings.Join([]string{
		message.Subject,
		message.Body,
		message.From.Name,
		message.From.Email,
		message.DisplayTo,
		message.DisplayCc,
	}, "\n"))
	if opts.Text != "" && !strings.Contains(haystack, strings.ToLower(strings.TrimSpace(opts.Text))) {
		return false
	}
	if opts.Subject != "" && !strings.Contains(strings.ToLower(message.Subject), strings.ToLower(strings.TrimSpace(opts.Subject))) {
		return false
	}
	if opts.From != "" {
		from := strings.ToLower(message.From.Name + "\n" + message.From.Email)
		if !strings.Contains(from, strings.ToLower(strings.TrimSpace(opts.From))) {
			return false
		}
	}
	if opts.To != "" {
		var recipients []string
		for _, mb := range append([]ews.Mailbox(nil), append(message.To, message.Cc...)...) {
			recipients = append(recipients, mb.Name, mb.Email)
		}
		if !strings.Contains(strings.ToLower(strings.Join(recipients, "\n")), strings.ToLower(strings.TrimSpace(opts.To))) {
			return false
		}
	}
	received := message.ReceivedAt
	if !opts.After.IsZero() && (received.IsZero() || received.Before(opts.After)) {
		return false
	}
	if !opts.Before.IsZero() && !received.IsZero() && !received.Before(opts.Before) {
		return false
	}
	if !opts.Since.IsZero() && (received.IsZero() || received.Before(opts.Since)) {
		return false
	}
	if !opts.Until.IsZero() && !received.IsZero() && received.After(opts.Until) {
		return false
	}
	return true
}

func decodeExchangeEWSMessage(message ews.Message) providerdata.EmailMessage {
	recipients := make([]string, 0, len(message.To)+len(message.Cc))
	for _, group := range [][]ews.Mailbox{message.To, message.Cc} {
		for _, mb := range group {
			formatted := strings.TrimSpace(mb.Name)
			if strings.TrimSpace(mb.Email) != "" {
				if formatted != "" {
					formatted += " <" + strings.TrimSpace(mb.Email) + ">"
				} else {
					formatted = strings.TrimSpace(mb.Email)
				}
			}
			if formatted != "" {
				recipients = append(recipients, formatted)
			}
		}
	}
	attachments := make([]providerdata.Attachment, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, providerdata.Attachment{
			Filename: attachment.Name,
			MimeType: attachment.ContentType,
			Size:     attachment.Size,
		})
	}
	sender := strings.TrimSpace(message.From.Name)
	if strings.TrimSpace(message.From.Email) != "" {
		if sender != "" {
			sender += " <" + strings.TrimSpace(message.From.Email) + ">"
		} else {
			sender = strings.TrimSpace(message.From.Email)
		}
	}
	body := strings.TrimSpace(message.Body)
	var bodyPtr *string
	if body != "" {
		bodyPtr = &body
	}
	return providerdata.EmailMessage{
		ID:          strings.TrimSpace(message.ID),
		ThreadID:    strings.TrimSpace(message.ConversationID),
		Subject:     strings.TrimSpace(message.Subject),
		Sender:      sender,
		Recipients:  recipients,
		Date:        message.ReceivedAt,
		Snippet:     snippetFromBody(message.Body),
		Labels:      nil,
		IsRead:      message.IsRead,
		BodyText:    bodyPtr,
		Attachments: attachments,
	}
}

func snippetFromBody(body string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if len(clean) <= 280 {
		return clean
	}
	return clean[:280]
}

func MarshalExchangeEWSConfig(cfg ExchangeEWSConfig) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	delete(out, "Password")
	delete(out, "password")
	return out, nil
}
