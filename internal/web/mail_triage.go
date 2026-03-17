package web

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/mailtriage"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type mailTriagePreviewRequest struct {
	MessageIDs     []string `json:"message_ids"`
	Folder         string   `json:"folder"`
	MaxResults     int64    `json:"max_results"`
	Phase          string   `json:"phase"`
	Apply          bool     `json:"apply"`
	IncludeBody    bool     `json:"include_body"`
	PrimaryBaseURL string   `json:"primary_base_url"`
	PrimaryModel   string   `json:"primary_model"`
	AuditBaseURL   string   `json:"audit_base_url"`
	AuditModel     string   `json:"audit_model"`
}

type mailTriageEvaluateRequest struct {
	PrimaryBaseURL string `json:"primary_base_url"`
	PrimaryModel   string `json:"primary_model"`
	Limit          int    `json:"limit"`
}

type mailTriageArmRequest struct {
	Apply bool `json:"apply"`
}

type mailTriageApplyRequest struct {
	Decisions []mailTriageApplyDecision `json:"decisions"`
}

type mailTriageApplyDecision struct {
	MessageID    string            `json:"message_id"`
	Action       mailtriage.Action `json:"action"`
	ArchiveLabel string            `json:"archive_label,omitempty"`
}

type mailServerFilterUpsertRequest struct {
	Filter email.ServerFilter `json:"filter"`
}

type mailTriageApplyResult struct {
	MessageID string            `json:"message_id"`
	Action    mailtriage.Action `json:"action"`
	Status    string            `json:"status"`
	Error     string            `json:"error,omitempty"`
}

func (a *App) handleMailTriagePreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	var req mailTriagePreviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	training, err := a.mailTriageTraining(account.ID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	var semantic mailtriage.Classifier
	if strings.TrimSpace(req.PrimaryBaseURL) != "" || strings.TrimSpace(req.PrimaryModel) != "" || strings.TrimSpace(a.intentLLMURL) != "" {
		semantic, err = a.mailTriageClassifier(req.PrimaryBaseURL, req.PrimaryModel)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var audit mailtriage.Classifier
	if strings.TrimSpace(req.AuditBaseURL) != "" || strings.TrimSpace(req.AuditModel) != "" {
		audit, err = a.mailTriageClassifier(req.AuditBaseURL, req.AuditModel)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	messages, err := a.loadMailTriageMessages(r.Context(), account, provider, req)
	if err != nil {
		a.writeMailProviderError(w, account, err)
		return
	}
	engine := mailtriage.Engine{
		Primary: mailtriage.HybridClassifier{
			Training: training.Model,
			Semantic: semantic,
		},
		Audit:  audit,
		Policy: mailtriage.DefaultPolicy(parseMailTriagePhase(req.Phase)),
	}
	results, err := engine.Evaluate(r.Context(), messages)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	applied := []mailTriageApplyResult(nil)
	if req.Apply && parseMailTriagePhase(req.Phase) == mailtriage.PhaseAutoApply {
		applied = a.applyMailTriageEvaluations(r.Context(), account, provider, results)
	}
	capabilities := email.ServerFilterCapabilities{Provider: account.Provider}
	if filterProvider, ok := provider.(email.ServerFilterProvider); ok {
		capabilities = filterProvider.ServerFilterCapabilities()
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":                    account,
		"results":                    results,
		"applied":                    applied,
		"server_filter_capabilities": capabilities,
	})
}

func (a *App) handleMailTriageReport(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	training, err := a.mailTriageTraining(account.ID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":  account,
		"training": training,
		"report":   training.Report,
		"warnings": training.Warnings,
		"rules":    training.DeterministicRules,
	})
}

func (a *App) handleMailTriageEvaluate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	var req mailTriageEvaluateRequest
	if err := decodeJSON(r, &req); err != nil && err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	training, err := a.mailTriageTraining(account.ID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if training.Model == nil {
		writeAPIData(w, http.StatusOK, map[string]any{
			"account": account,
			"report":  training.Report,
			"results": []any{},
		})
		return
	}
	var semantic mailtriage.Classifier
	if strings.TrimSpace(req.PrimaryBaseURL) != "" || strings.TrimSpace(req.PrimaryModel) != "" {
		semantic, err = a.mailTriageClassifier(req.PrimaryBaseURL, req.PrimaryModel)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	limit := req.Limit
	if limit <= 0 || limit > 2000 {
		limit = 1000
	}
	reviews, err := a.store.ListMailTriageReviews(account.ID, limit)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	engine := mailtriage.Engine{
		Primary: mailtriage.HybridClassifier{
			Training: training.Model,
			Semantic: semantic,
		},
		Policy: mailtriage.DefaultPolicy(mailtriage.PhaseShadow),
	}
	messages := make([]mailtriage.Message, 0, len(reviews))
	actual := make(map[string]mailtriage.Action, len(reviews))
	for _, review := range reviews {
		message := mailtriage.Message{
			ID:            strings.TrimSpace(review.MessageID),
			Provider:      account.Provider,
			AccountLabel:  account.Label,
			Subject:       strings.TrimSpace(review.Subject),
			Sender:        strings.TrimSpace(review.Sender),
			Labels:        compactStringList([]string{review.Folder}),
			ReviewCount:   training.ReviewCount,
			PolicySummary: append([]string(nil), training.PolicySummary...),
			Examples:      append([]mailtriage.Example(nil), training.Examples...),
		}
		messages = append(messages, message)
		actual[message.ID] = normalizeMailTriageAction(review.Action)
	}
	results, err := engine.Evaluate(r.Context(), messages)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	confusion := map[string]map[string]int{}
	perAction := map[string]map[string]int{}
	for _, result := range results {
		want := string(actual[result.Message.ID])
		got := string(result.Primary.Action)
		if confusion[want] == nil {
			confusion[want] = map[string]int{}
		}
		confusion[want][got]++
		if perAction[got] == nil {
			perAction[got] = map[string]int{}
		}
		perAction[got]["predicted"]++
		if want == got {
			perAction[got]["correct"]++
		}
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":    account,
		"report":     training.Report,
		"results":    results,
		"confusion":  confusion,
		"per_action": perAction,
	})
}

func (a *App) handleMailTriageArm(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	var req mailTriageArmRequest
	if err := decodeJSON(r, &req); err != nil && err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	training, err := a.mailTriageTraining(account.ID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	reviews, err := a.store.ListMailTriageReviews(account.ID, 1000)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	filters := recommendedMailTriageServerFilters(account.Provider, reviews, training)
	if !req.Apply {
		writeAPIData(w, http.StatusOK, map[string]any{
			"account": account,
			"filters": filters,
			"report":  training.Report,
		})
		return
	}
	existing, err := filterProvider.ListServerFilters(r.Context())
	if err != nil {
		a.writeMailProviderError(w, account, err)
		return
	}
	existingByName := map[string]email.ServerFilter{}
	for _, filter := range existing {
		existingByName[strings.ToLower(strings.TrimSpace(filter.Name))] = filter
	}
	applied := make([]email.ServerFilter, 0, len(filters))
	for _, filter := range filters {
		if current, ok := existingByName[strings.ToLower(strings.TrimSpace(filter.Name))]; ok {
			filter.ID = current.ID
		}
		saved, err := filterProvider.UpsertServerFilter(r.Context(), filter)
		if err != nil {
			a.writeMailProviderError(w, account, err)
			return
		}
		applied = append(applied, saved)
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account": account,
		"filters": applied,
		"report":  training.Report,
	})
}

func (a *App) handleMailTriageApply(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	var req mailTriageApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	results := a.applyMailTriageDecisions(r.Context(), account, provider, req.Decisions)
	writeAPIData(w, http.StatusOK, map[string]any{"results": results})
}

func (a *App) handleMailServerFiltersList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	filters, err := filterProvider.ListServerFilters(r.Context())
	if err != nil {
		a.writeMailProviderError(w, account, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":      account,
		"filters":      filters,
		"capabilities": filterProvider.ServerFilterCapabilities(),
	})
}

func (a *App) handleMailServerFilterUpsert(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	var req mailServerFilterUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	filterID := strings.TrimSpace(chi.URLParam(r, "filter_id"))
	if filterID != "" {
		req.Filter.ID = filterID
	}
	filter, err := filterProvider.UpsertServerFilter(r.Context(), req.Filter)
	if err != nil {
		a.writeMailProviderError(w, account, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"filter": filter})
}

func (a *App) handleMailServerFilterDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	if err := a.guardMailAccountBackoff(account); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	filterID := strings.TrimSpace(chi.URLParam(r, "filter_id"))
	if filterID == "" {
		writeAPIError(w, http.StatusBadRequest, "filter_id is required")
		return
	}
	if err := filterProvider.DeleteServerFilter(r.Context(), filterID); err != nil {
		a.writeMailProviderError(w, account, err)
		return
	}
	writeNoContent(w)
}

func (a *App) emailProviderForRoute(ctx context.Context, r *http.Request) (store.ExternalAccount, email.EmailProvider, error) {
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := a.emailProviderForAccount(ctx, account, cfg)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (a *App) mailTriageClassifier(baseURL, model string) (mailtriage.Classifier, error) {
	resolvedBaseURL := strings.TrimSpace(baseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = strings.TrimSpace(a.intentLLMURL)
	}
	if resolvedBaseURL == "" {
		return nil, errBadRequest("mail triage classifier base URL is required")
	}
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" {
		resolvedModel = a.localIntentLLMModel()
	}
	return mailtriage.OpenAIClassifier{
		BaseURL: resolvedBaseURL,
		Model:   resolvedModel,
	}, nil
}

func (a *App) loadMailTriageMessages(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, req mailTriagePreviewRequest) ([]mailtriage.Message, error) {
	ids := compactStringList(req.MessageIDs)
	if len(ids) == 0 {
		opts := email.DefaultSearchOptions().WithMaxResults(req.MaxResults)
		folder := strings.TrimSpace(req.Folder)
		if folder == "" {
			folder = "inbox"
		}
		opts = opts.WithFolder(folder)
		var err error
		ids, err = provider.ListMessages(ctx, opts)
		if err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	messages, err := provider.GetMessages(ctx, ids, "")
	if err != nil {
		return nil, err
	}
	cfg, _ := decodeEmailSyncAccountConfig(account)
	accountAddress := firstNonEmpty(cfg.FromAddress, cfg.Username, account.AccountName)
	training, err := a.mailTriageTraining(account.ID)
	if err != nil {
		return nil, err
	}
	out := make([]mailtriage.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		out = append(out, toMailTriageMessage(account, accountAddress, req.IncludeBody, message, training))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReceivedAt.After(out[j].ReceivedAt)
	})
	return out, nil
}

func (a *App) mailTriageTraining(accountID int64) (mailtriage.DistilledTraining, error) {
	reviews, err := a.store.ListMailTriageReviews(accountID, 1000)
	if err != nil {
		return mailtriage.DistilledTraining{}, err
	}
	input := make([]mailtriage.ReviewedExample, 0, len(reviews))
	for _, review := range reviews {
		input = append(input, mailtriage.ReviewedExample{
			Sender:  strings.TrimSpace(review.Sender),
			Subject: strings.TrimSpace(review.Subject),
			Folder:  strings.TrimSpace(review.Folder),
			Action:  strings.TrimSpace(review.Action),
		})
	}
	return mailtriage.DistillReviewedExamples(input), nil
}

func toMailTriageMessage(account store.ExternalAccount, accountAddress string, includeBody bool, message *providerdata.EmailMessage, training mailtriage.DistilledTraining) mailtriage.Message {
	body := ""
	if includeBody {
		if message.BodyText != nil {
			body = strings.TrimSpace(*message.BodyText)
		}
	}
	return mailtriage.Message{
		ID:             strings.TrimSpace(message.ID),
		Provider:       account.Provider,
		AccountLabel:   account.Label,
		AccountAddress: strings.TrimSpace(accountAddress),
		ThreadID:       strings.TrimSpace(message.ThreadID),
		Subject:        strings.TrimSpace(message.Subject),
		Sender:         strings.TrimSpace(message.Sender),
		Recipients:     compactStringList(message.Recipients),
		Labels:         compactStringList(message.Labels),
		Snippet:        strings.TrimSpace(message.Snippet),
		Body:           body,
		HasAttachments: len(message.Attachments) > 0,
		IsRead:         message.IsRead,
		IsFlagged:      message.IsFlagged,
		ReceivedAt:     message.Date,
		ReviewCount:    training.ReviewCount,
		PolicySummary:  append([]string(nil), training.PolicySummary...),
		Examples:       append([]mailtriage.Example(nil), training.Examples...),
		LocalHints:     append([]string(nil), training.Warnings...),
		ProtectedTopic: false,
		AgeDays:        max(0, int(time.Since(message.Date).Hours()/24)),
	}
}

func normalizeMailTriageAction(raw string) mailtriage.Action {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inbox":
		return mailtriage.ActionInbox
	case "cc":
		return mailtriage.ActionCC
	case "archive":
		return mailtriage.ActionArchive
	case "trash":
		return mailtriage.ActionTrash
	default:
		return ""
	}
}

func recommendedMailTriageServerFilters(provider string, reviews []store.MailTriageReview, training mailtriage.DistilledTraining) []email.ServerFilter {
	_ = provider
	filters := make([]email.ServerFilter, 0, len(training.DeterministicRules)+2)
	for _, rule := range training.DeterministicRules {
		if rule.Scope != "sender" {
			continue
		}
		switch rule.Action {
		case mailtriage.ActionTrash:
			filters = append(filters, email.ServerFilter{
				Name:    "tabura/triage/sender-trash/" + sanitizeFilterName(rule.Key),
				Enabled: true,
				Criteria: email.ServerFilterCriteria{
					From: rule.Key,
				},
				Action: email.ServerFilterAction{Trash: true},
			})
		case mailtriage.ActionCC:
			filters = append(filters, email.ServerFilter{
				Name:    "tabura/triage/sender-cc/" + sanitizeFilterName(rule.Key),
				Enabled: true,
				Criteria: email.ServerFilterCriteria{
					From: rule.Key,
				},
				Action: email.ServerFilterAction{MoveTo: "CC"},
			})
		}
	}
	if subject := repeatedSubjectForSender(reviews, "system@online.tugraz.at", "trash", "lv-evaluierung"); subject != "" {
		filters = append(filters, email.ServerFilter{
			Name:    "tabura/triage/tugrazonline-evaluation-trash",
			Enabled: true,
			Criteria: email.ServerFilterCriteria{
				From:    "system@online.tugraz.at",
				Subject: subject,
			},
			Action: email.ServerFilterAction{Trash: true},
		})
	}
	return dedupeServerFilters(filters)
}

func repeatedSubjectForSender(reviews []store.MailTriageReview, sender, action, snippet string) string {
	targetSender := strings.ToLower(strings.TrimSpace(sender))
	targetAction := strings.ToLower(strings.TrimSpace(action))
	targetSnippet := strings.ToLower(strings.TrimSpace(snippet))
	counts := map[string]int{}
	for _, review := range reviews {
		if strings.ToLower(strings.TrimSpace(review.Action)) != targetAction {
			continue
		}
		if !strings.Contains(strings.ToLower(normalizeMailSenderForFilter(review.Sender)), targetSender) {
			continue
		}
		subject := strings.TrimSpace(review.Subject)
		if !strings.Contains(strings.ToLower(subject), targetSnippet) {
			continue
		}
		counts[subject]++
	}
	bestSubject := ""
	bestCount := 0
	for subject, count := range counts {
		if count > bestCount {
			bestSubject = subject
			bestCount = count
		}
	}
	if bestCount < 2 {
		return ""
	}
	return bestSubject
}

func dedupeServerFilters(filters []email.ServerFilter) []email.ServerFilter {
	out := make([]email.ServerFilter, 0, len(filters))
	seen := map[string]struct{}{}
	for _, filter := range filters {
		key := strings.ToLower(strings.TrimSpace(filter.Name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, filter)
	}
	return out
}

func sanitizeFilterName(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer("@", "_at_", ".", "_", "<", "", ">", "", " ", "_")
	clean = replacer.Replace(clean)
	clean = strings.Trim(clean, "_")
	if clean == "" {
		return "rule"
	}
	return clean
}

func normalizeMailSenderForFilter(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if idx := strings.LastIndex(clean, "<"); idx >= 0 && strings.HasSuffix(clean, ">") {
		return strings.TrimSpace(clean[idx+1 : len(clean)-1])
	}
	return clean
}

func parseMailTriagePhase(raw string) mailtriage.Phase {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(mailtriage.PhaseShadow):
		return mailtriage.PhaseShadow
	case string(mailtriage.PhaseAutoApply):
		return mailtriage.PhaseAutoApply
	default:
		return mailtriage.PhaseManualReview
	}
}

func (a *App) applyMailTriageEvaluations(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, evals []mailtriage.Evaluation) []mailTriageApplyResult {
	decisions := make([]mailTriageApplyDecision, 0, len(evals))
	for _, eval := range evals {
		if eval.Disposition != mailtriage.DispositionAutoApply {
			continue
		}
		decisions = append(decisions, mailTriageApplyDecision{
			MessageID:    eval.Message.ID,
			Action:       eval.Primary.Action,
			ArchiveLabel: eval.Primary.ArchiveLabel,
		})
	}
	return a.applyMailTriageDecisions(ctx, account, provider, decisions)
}

func (a *App) applyMailTriageDecisions(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, decisions []mailTriageApplyDecision) []mailTriageApplyResult {
	type key struct {
		action mailtriage.Action
		label  string
	}
	groups := map[key][]string{}
	order := make([]key, 0, len(decisions))
	for _, decision := range decisions {
		messageID := strings.TrimSpace(decision.MessageID)
		if messageID == "" {
			continue
		}
		groupKey := key{action: decision.Action, label: strings.TrimSpace(decision.ArchiveLabel)}
		if _, ok := groups[groupKey]; !ok {
			order = append(order, groupKey)
		}
		groups[groupKey] = append(groups[groupKey], messageID)
	}
	results := make([]mailTriageApplyResult, 0, len(decisions))
	for _, groupKey := range order {
		ids := groups[groupKey]
		err := a.applyMailTriageAction(ctx, account, provider, groupKey.action, groupKey.label, ids)
		status := "ok"
		errText := ""
		if err != nil {
			status = "error"
			errText = err.Error()
		}
		for _, id := range ids {
			results = append(results, mailTriageApplyResult{
				MessageID: id,
				Action:    groupKey.action,
				Status:    status,
				Error:     errText,
			})
		}
	}
	return results
}

func (a *App) applyMailTriageAction(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, action mailtriage.Action, archiveLabel string, messageIDs []string) error {
	cmd := mailActionCommand{
		MessageIDs: messageIDs,
	}
	switch action {
	case mailtriage.ActionInbox:
		cmd.Action = "move_to_inbox"
	case mailtriage.ActionTrash:
		cmd.Action = "trash"
	case mailtriage.ActionCC:
		cmd.Action = "move_to_folder"
		cmd.Folder = "CC"
	case mailtriage.ActionArchive:
		if label := strings.TrimSpace(archiveLabel); label != "" {
			cmd.Action = "archive_label"
			cmd.Label = label
		} else {
			cmd.Action = "archive"
		}
	default:
		return errBadRequest("unsupported triage action")
	}
	_, err := a.executeMailAction(ctx, account, provider, cmd)
	return err
}

func compactStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}
