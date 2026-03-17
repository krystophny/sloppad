package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/mailtriage"
	"github.com/krystophny/tabura/internal/store"
)

func TestMailTriageLiveExchangeSmoke(t *testing.T) {
	if os.Getenv("TABURA_LIVE_TRIAGE_SMOKE") != "1" && os.Getenv("TABURA_LIVE_TRIAGE_ARM") != "1" {
		t.Skip("set TABURA_LIVE_TRIAGE_SMOKE=1 to run against the live Exchange account")
	}
	app, account := liveTriageAppAndAccount(t)
	defer func() {
		_ = app.Shutdown(context.Background())
	}()
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		t.Fatalf("decodeEmailSyncAccountConfig() error: %v", err)
	}
	provider, err := app.emailProviderForAccount(context.Background(), account, cfg)
	if err != nil {
		t.Fatalf("emailProviderForAccount() error: %v", err)
	}
	defer provider.Close()
	ids, err := provider.ListMessages(context.Background(), email.DefaultSearchOptions().WithFolder("Posteingang").WithMaxResults(3))
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("live inbox returned no message IDs")
	}
	rawMessages, err := provider.GetMessages(context.Background(), ids, "")
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	training, err := app.mailTriageTraining(account.ID)
	if err != nil {
		t.Fatalf("mailTriageTraining() error: %v", err)
	}
	classifier := mailtriage.HybridClassifier{Training: training.Model}
	for _, rawMessage := range rawMessages {
		if rawMessage == nil {
			continue
		}
		message := toMailTriageMessage(account, "", false, rawMessage, training)
		decision, err := classifier.Classify(context.Background(), message)
		if err != nil {
			t.Fatalf("Classify(%s) error: %v", rawMessage.ID, err)
		}
		if decision.Action == "" {
			t.Fatalf("Classify(%s) returned empty action", rawMessage.ID)
		}
	}
}

func TestMailTriageLiveExchangeArmDeterministicFilters(t *testing.T) {
	if os.Getenv("TABURA_LIVE_TRIAGE_ARM") != "1" {
		t.Skip("set TABURA_LIVE_TRIAGE_ARM=1 to upsert managed deterministic filters on the live Exchange account")
	}
	app, account := liveTriageAppAndAccount(t)
	defer func() {
		_ = app.Shutdown(context.Background())
	}()
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		t.Fatalf("decodeEmailSyncAccountConfig() error: %v", err)
	}
	provider, err := app.emailProviderForAccount(context.Background(), account, cfg)
	if err != nil {
		t.Fatalf("emailProviderForAccount() error: %v", err)
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		t.Fatal("live exchange provider does not expose ServerFilterProvider")
	}
	reviews, err := app.store.ListMailTriageReviews(account.ID, 1000)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	training, err := app.mailTriageTraining(account.ID)
	if err != nil {
		t.Fatalf("mailTriageTraining() error: %v", err)
	}
	filters := recommendedMailTriageServerFilters(account.Provider, reviews, training)
	if len(filters) == 0 {
		t.Fatal("recommendedMailTriageServerFilters() returned no filters")
	}
	for _, filter := range filters {
		t.Logf("managed filter candidate: %s", filter.Name)
	}
	existing, err := filterProvider.ListServerFilters(context.Background())
	if err != nil {
		t.Fatalf("ListServerFilters() error: %v", err)
	}
	existingByName := map[string]email.ServerFilter{}
	for _, filter := range existing {
		existingByName[strings.ToLower(strings.TrimSpace(filter.Name))] = filter
	}
	for _, filter := range filters {
		if current, ok := existingByName[strings.ToLower(strings.TrimSpace(filter.Name))]; ok {
			filter.ID = current.ID
		}
		if _, err := filterProvider.UpsertServerFilter(context.Background(), filter); err != nil {
			t.Fatalf("UpsertServerFilter(%s) error: %v", filter.Name, err)
		}
	}
	after, err := filterProvider.ListServerFilters(context.Background())
	if err != nil {
		t.Fatalf("ListServerFilters(after) error: %v", err)
	}
	for _, expected := range filters {
		found := false
		for _, actual := range after {
			if strings.EqualFold(strings.TrimSpace(actual.Name), strings.TrimSpace(expected.Name)) {
				t.Logf("managed filter present: %s", actual.Name)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("managed filter %q was not present after upsert", expected.Name)
		}
	}
}

func liveTriageAppAndAccount(t *testing.T) (*App, store.ExternalAccount) {
	t.Helper()
	dataDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "tabura-web")
	app, err := New(dataDir, "/home/ert/code/assi/tabura", "http://127.0.0.1:9420/mcp", DefaultAppServerURL, "", DefaultTTSURL, "", true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	accounts, err := app.store.ListExternalAccounts(store.SphereWork)
	if err != nil {
		_ = app.Shutdown(context.Background())
		t.Fatalf("ListExternalAccounts() error: %v", err)
	}
	var account store.ExternalAccount
	for _, candidate := range accounts {
		if strings.EqualFold(candidate.Provider, store.ExternalProviderExchangeEWS) {
			account = candidate
			break
		}
	}
	if account.ID <= 0 {
		_ = app.Shutdown(context.Background())
		t.Fatal("no live exchange_ews account found")
	}
	return app, account
}
