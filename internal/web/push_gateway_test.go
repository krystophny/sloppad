package web

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestAPNSGatewaySendFormatsRequest(t *testing.T) {
	privateKeyPEM, err := generateTestECPrivateKeyPEM()
	if err != nil {
		t.Fatalf("generateTestECPrivateKeyPEM() error: %v", err)
	}
	privateKey, err := parseECPrivateKey(privateKeyPEM)
	if err != nil {
		t.Fatalf("parseECPrivateKey() error: %v", err)
	}

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/device/device-1" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/3/device/device-1")
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "bearer ") {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if got := r.Header.Get("apns-topic"); got != "dev.tabura" {
			t.Fatalf("apns-topic = %q, want %q", got, "dev.tabura")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := &apnsGateway{
		keyID:      "kid",
		teamID:     "team",
		topic:      "dev.tabura",
		baseURL:    server.URL,
		privateKey: privateKey,
		client:     server.Client(),
		now:        func() time.Time { return time.Unix(1710000000, 0) },
	}
	if err := gateway.Send(context.Background(), store.PushRegistration{
		Platform:    "apns",
		DeviceToken: "device-1",
	}, pushNotification{
		Title: "Title",
		Body:  "Body",
		Data:  map[string]string{"kind": "assistant_turn_completed"},
	}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	aps, ok := gotBody["aps"].(map[string]any)
	if !ok {
		t.Fatalf("aps payload = %#v", gotBody["aps"])
	}
	alert, ok := aps["alert"].(map[string]any)
	if !ok {
		t.Fatalf("alert payload = %#v", aps["alert"])
	}
	if got := strFromAny(alert["title"]); got != "Title" {
		t.Fatalf("title = %q, want %q", got, "Title")
	}
}

func TestFCMGatewaySendFormatsRequest(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error: %v", err)
	}
	var (
		tokenHits int
		sendHits  int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenHits++
			if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
				t.Fatalf("token content type = %q", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"token-123","expires_in":3600}`))
		case "/send":
			sendHits++
			if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
				t.Fatalf("authorization = %q, want %q", got, "Bearer token-123")
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode send payload: %v", err)
			}
			message, ok := payload["message"].(map[string]any)
			if !ok {
				t.Fatalf("message payload = %#v", payload["message"])
			}
			if got := strFromAny(message["token"]); got != "device-2" {
				t.Fatalf("message.token = %q, want %q", got, "device-2")
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	gateway := &fcmGateway{
		projectID:   "proj-1",
		tokenURL:    server.URL + "/token",
		endpointURL: server.URL + "/send",
		clientEmail: "svc@example.com",
		privateKey:  privateKey,
		client:      server.Client(),
		now:         func() time.Time { return time.Unix(1710000000, 0) },
	}
	if err := gateway.Send(context.Background(), store.PushRegistration{
		Platform:    "fcm",
		DeviceToken: "device-2",
	}, pushNotification{
		Title: "Title",
		Body:  "Body",
		Data:  map[string]string{"kind": "calendar_event_start"},
	}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if tokenHits != 1 {
		t.Fatalf("token hits = %d, want 1", tokenHits)
	}
	if sendHits != 1 {
		t.Fatalf("send hits = %d, want 1", sendHits)
	}
}
