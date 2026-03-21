package web

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	tabcalendar "github.com/krystophny/tabura/internal/calendar"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type fakePushGateway struct {
	sends []store.PushRegistration
	last  pushNotification
}

func (f *fakePushGateway) Send(_ context.Context, reg store.PushRegistration, notification pushNotification) error {
	f.sends = append(f.sends, reg)
	f.last = notification
	return nil
}

type stubPushGoogleClient struct {
	calendars []providerdata.Calendar
	events    []providerdata.Event
}

func (s stubPushGoogleClient) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s stubPushGoogleClient) GetEvents(context.Context, tabcalendar.GetEventsOptions) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events...), nil
}

func (s stubPushGoogleClient) CreateEvent(context.Context, tabcalendar.CreateEventOptions) (providerdata.Event, error) {
	return providerdata.Event{}, nil
}

func (s stubPushGoogleClient) UpdateEvent(context.Context, tabcalendar.UpdateEventOptions) (providerdata.Event, error) {
	return providerdata.Event{}, nil
}

func (s stubPushGoogleClient) DeleteEvent(context.Context, string, string) error {
	return nil
}

type fakeMDNSAdvertiser struct {
	shutdowns int
}

func (f *fakeMDNSAdvertiser) Shutdown() {
	f.shutdowns++
}

func TestHandlePushRegisterCreatesSessionScopedRegistration(t *testing.T) {
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Desk", filepath.Join(t.TempDir(), "desk"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/push/register", map[string]any{
		"session_id":   session.ID,
		"platform":     "apns",
		"device_token": "device-1",
		"device_label": "iPhone",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	data := decodeJSONDataResponse(t, rr)
	registration, ok := data["registration"].(map[string]any)
	if !ok {
		t.Fatalf("registration payload = %#v", data["registration"])
	}
	if got := strFromAny(registration["session_id"]); got != session.ID {
		t.Fatalf("session_id = %q, want %q", got, session.ID)
	}
	if got := intFromAny(registration["workspace_id"], 0); got != int(workspace.ID) {
		t.Fatalf("workspace_id = %d, want %d", got, workspace.ID)
	}
}

func TestFinalizeAssistantResponseSendsPushOnlyWithoutChatClient(t *testing.T) {
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Desk", filepath.Join(t.TempDir(), "desk"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace() error: %v", err)
	}
	if _, err := app.store.UpsertPushRegistration(store.PushRegistration{
		SessionID:   session.ID,
		WorkspaceID: workspace.ID,
		Platform:    "fcm",
		DeviceToken: "device-1",
	}); err != nil {
		t.Fatalf("UpsertPushRegistration() error: %v", err)
	}

	gateway := &fakePushGateway{}
	app.pushService = &pushService{
		store:            app.store,
		gateway:          gateway,
		now:              time.Now,
		pollInterval:     time.Minute,
		calendarLeadTime: time.Minute,
		sentCalendar:     map[string]time.Time{},
	}

	var persistedID int64
	persistedText := ""
	app.finalizeAssistantResponseWithMetadata(
		session.ID,
		workspace.DirPath,
		"Assistant reply",
		&persistedID,
		&persistedText,
		"turn-1",
		"",
		"",
		"text",
		assistantResponseMetadata{},
	)
	if len(gateway.sends) != 1 {
		t.Fatalf("push sends = %d, want 1", len(gateway.sends))
	}

	conn := &chatWSConn{}
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)
	app.maybeNotifyCompletedTurn(session.ID, workspace.DirPath, "Second reply")
	if len(gateway.sends) != 1 {
		t.Fatalf("push sends with connected websocket = %d, want 1", len(gateway.sends))
	}
}

func TestPushSchedulerSendsUpcomingCalendarEvent(t *testing.T) {
	app := newAuthedTestApp(t)
	now := time.Date(2026, 3, 21, 14, 0, 0, 0, time.Local)
	if _, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Primary", nil); err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	if _, err := app.store.UpsertPushRegistration(store.PushRegistration{
		Platform:    "apns",
		DeviceToken: "device-1",
	}); err != nil {
		t.Fatalf("UpsertPushRegistration() error: %v", err)
	}

	gateway := &fakePushGateway{}
	app.pushService = &pushService{
		store:            app.store,
		gateway:          gateway,
		now:              func() time.Time { return now },
		pollInterval:     time.Minute,
		calendarLeadTime: 90 * time.Second,
		sentCalendar:     map[string]time.Time{},
	}
	app.calendarNow = func() time.Time { return now }
	app.newGoogleCalendarClient = func(context.Context) (googleCalendarClient, error) {
		return stubPushGoogleClient{
			calendars: []providerdata.Calendar{{ID: "cal-1", Name: "Primary", Primary: true}},
			events: []providerdata.Event{{
				ID:         "evt-1",
				CalendarID: "cal-1",
				Summary:    "Standup",
				Start:      now.Add(45 * time.Second),
				End:        now.Add(30 * time.Minute),
			}},
		}, nil
	}

	app.pushService.deliverDueCalendarEvents(context.Background(), app)
	if len(gateway.sends) != 1 {
		t.Fatalf("scheduled push sends = %d, want 1", len(gateway.sends))
	}
	if got := gateway.last.Data["kind"]; got != "calendar_event_start" {
		t.Fatalf("push kind = %q, want %q", got, "calendar_event_start")
	}

	app.pushService.deliverDueCalendarEvents(context.Background(), app)
	if len(gateway.sends) != 1 {
		t.Fatalf("duplicate scheduled push sends = %d, want 1", len(gateway.sends))
	}
}

func TestMDNSAdvertisementStartsOnlyForLANBinds(t *testing.T) {
	app := newAuthedTestApp(t)
	var started int
	var advertiser *fakeMDNSAdvertiser
	app.mdnsAdvertiserFactory = func(name string, port int, txt []string) (mdnsAdvertiser, error) {
		started++
		advertiser = &fakeMDNSAdvertiser{}
		if port != 8420 {
			t.Fatalf("port = %d, want 8420", port)
		}
		if len(txt) == 0 {
			t.Fatal("expected txt records")
		}
		return advertiser, nil
	}

	if err := app.startMDNSAdvertisement("127.0.0.1", 8420); err != nil {
		t.Fatalf("startMDNSAdvertisement(loopback) error: %v", err)
	}
	if started != 0 {
		t.Fatalf("started for loopback = %d, want 0", started)
	}

	if err := app.startMDNSAdvertisement("0.0.0.0", 8420); err != nil {
		t.Fatalf("startMDNSAdvertisement(public) error: %v", err)
	}
	if started != 1 {
		t.Fatalf("started for public bind = %d, want 1", started)
	}
	app.stopMDNSAdvertisement()
	if advertiser == nil || advertiser.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", advertiser.shutdowns)
	}
}
