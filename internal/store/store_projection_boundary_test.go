package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestProjectionBoundaryScrubsRemoteBodiesOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Work Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	taskWorkspace, err := s.CreateWorkspace("Exchange Tasks", filepath.Join(t.TempDir(), "tasks"), SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	emailTitle := "Contract review"
	emailMeta := `{"subject":"Contract review","sender":"alice@example.com","body":"Please review the contract summary."}`
	emailArtifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &emailTitle, &emailMeta)
	if err != nil {
		t.Fatalf("CreateArtifact(email) error: %v", err)
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: "email",
		RemoteID:   "msg-1",
		ArtifactID: &emailArtifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(email) error: %v", err)
	}

	threadTitle := "Discussion"
	threadMeta := `{"thread_id":"thread-1","messages":[{"id":"msg-1","sender":"alice@example.com","body":"First message body"}]}`
	threadArtifact, err := s.CreateArtifact(ArtifactKindEmailThread, nil, nil, &threadTitle, &threadMeta)
	if err != nil {
		t.Fatalf("CreateArtifact(email_thread) error: %v", err)
	}
	if _, err := s.UpsertExternalBinding(ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: "email_thread",
		RemoteID:   "thread-1",
		ArtifactID: &threadArtifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(email_thread) error: %v", err)
	}

	taskTitle := "Exchange Task"
	taskMeta := `{"subject":"Exchange Task","status":"NotStarted","body":"Full upstream task body"}`
	taskArtifact, err := s.CreateArtifact(ArtifactKindExternalTask, nil, nil, &taskTitle, &taskMeta)
	if err != nil {
		t.Fatalf("CreateArtifact(external_task) error: %v", err)
	}
	taskSource := ExternalProviderExchangeEWS
	taskSourceRef := "task:ews-1"
	if _, err := s.CreateItem(taskTitle, ItemOptions{
		WorkspaceID: &taskWorkspace.ID,
		ArtifactID:  &taskArtifact.ID,
		Source:      &taskSource,
		SourceRef:   &taskSourceRef,
	}); err != nil {
		t.Fatalf("CreateItem(exchange task) error: %v", err)
	}

	localEmailTitle := "Local Draft"
	localEmailMeta := `{"subject":"Local Draft","body":"Keep this local body"}`
	localEmailArtifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &localEmailTitle, &localEmailMeta)
	if err != nil {
		t.Fatalf("CreateArtifact(local email) error: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	reopened, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() {
		_ = reopened.Close()
	}()

	assertNoBodyWithPreview := func(t *testing.T, artifactID int64, previewKey, want string) {
		t.Helper()
		artifact, err := reopened.GetArtifact(artifactID)
		if err != nil {
			t.Fatalf("GetArtifact(%d) error: %v", artifactID, err)
		}
		var meta map[string]any
		if err := json.Unmarshal([]byte(*artifact.MetaJSON), &meta); err != nil {
			t.Fatalf("unmarshal artifact %d meta: %v", artifactID, err)
		}
		if _, ok := meta["body"]; ok {
			t.Fatalf("artifact %d still stores body: %#v", artifactID, meta)
		}
		if got := projectionString(meta[previewKey]); got != want {
			t.Fatalf("artifact %d %s = %q, want %q", artifactID, previewKey, got, want)
		}
	}

	assertNoBodyWithPreview(t, emailArtifact.ID, "snippet", "Please review the contract summary.")
	assertNoBodyWithPreview(t, taskArtifact.ID, "summary", "Full upstream task body")

	threadArtifactAfter, err := reopened.GetArtifact(threadArtifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(thread) error: %v", err)
	}
	var threadPayload map[string]any
	if err := json.Unmarshal([]byte(*threadArtifactAfter.MetaJSON), &threadPayload); err != nil {
		t.Fatalf("unmarshal thread meta: %v", err)
	}
	messages, _ := threadPayload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("thread messages = %#v, want 1 entry", threadPayload["messages"])
	}
	message, _ := messages[0].(map[string]any)
	if _, ok := message["body"]; ok {
		t.Fatalf("thread message still stores body: %#v", message)
	}
	if got := projectionString(message["snippet"]); got != "First message body" {
		t.Fatalf("thread message snippet = %q, want %q", got, "First message body")
	}

	localArtifactAfter, err := reopened.GetArtifact(localEmailArtifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(local email) error: %v", err)
	}
	var localMeta map[string]any
	if err := json.Unmarshal([]byte(*localArtifactAfter.MetaJSON), &localMeta); err != nil {
		t.Fatalf("unmarshal local email meta: %v", err)
	}
	if got := projectionString(localMeta["body"]); got != "Keep this local body" {
		t.Fatalf("local artifact body = %q, want preserved local body", got)
	}
}
