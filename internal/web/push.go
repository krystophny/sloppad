package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	pushSchedulePollInterval = time.Minute
	pushScheduleLeadTime     = 75 * time.Second
	pushSendTimeout          = 5 * time.Second
	pushBodyLimit            = 160
)

type pushNotification struct {
	Title string
	Body  string
	Data  map[string]string
}

type pushGateway interface {
	Send(context.Context, store.PushRegistration, pushNotification) error
}

type pushService struct {
	store            *store.Store
	gateway          pushGateway
	now              func() time.Time
	pollInterval     time.Duration
	calendarLeadTime time.Duration

	mu           sync.Mutex
	sentCalendar map[string]time.Time
}

func newPushService(s *store.Store) *pushService {
	return &pushService{
		store:            s,
		gateway:          newEnvPushGateway(),
		now:              time.Now,
		pollInterval:     pushSchedulePollInterval,
		calendarLeadTime: pushScheduleLeadTime,
		sentCalendar:     map[string]time.Time{},
	}
}

func (p *pushService) notifyTurnCompletion(ctx context.Context, sessionID string, workspaceID int64, workspaceName string, text string) error {
	if p == nil || p.store == nil || p.gateway == nil {
		return nil
	}
	registrations, err := p.store.ListPushRegistrationsForChatSession(sessionID, workspaceID)
	if err != nil {
		return err
	}
	if len(registrations) == 0 {
		return nil
	}
	title := "Tabura reply"
	if clean := strings.TrimSpace(workspaceName); clean != "" {
		title = "Tabura reply for " + clean
	}
	return p.send(ctx, registrations, pushNotification{
		Title: title,
		Body:  summarizePushBody(text),
		Data: map[string]string{
			"kind":       "assistant_turn_completed",
			"session_id": strings.TrimSpace(sessionID),
		},
	})
}

func (p *pushService) deliverDueCalendarEvents(ctx context.Context, app *App) {
	if p == nil || p.store == nil || p.gateway == nil || app == nil {
		return
	}
	registrations, err := p.store.ListPushRegistrations()
	if err != nil {
		log.Printf("push scheduler: list registrations: %v", err)
		return
	}
	if len(registrations) == 0 {
		return
	}
	now := p.now()
	activeSphere, err := app.store.ActiveSphere()
	if err != nil || strings.TrimSpace(activeSphere) == "" {
		activeSphere = store.SpherePrivate
	}
	req := calendarActionRequest{View: calendarViewAgenda, Date: now.In(time.Local)}
	events, warnings, err := app.collectCalendarEvents(ctx, req, activeSphere)
	if err != nil {
		log.Printf("push scheduler: collect calendar events: %v", err)
		return
	}
	for _, warning := range warnings {
		log.Printf("push scheduler: %s", warning)
	}
	windowEnd := now.Add(p.calendarLeadTime)
	for _, event := range events {
		if event.AllDay {
			continue
		}
		start := event.Start.In(time.Local)
		if start.Before(now) || start.After(windowEnd) {
			continue
		}
		if !p.markCalendarEventSent(calendarEventKey(event), now.Add(2*p.calendarLeadTime)) {
			continue
		}
		if err := p.send(ctx, registrations, pushNotification{
			Title: "Upcoming: " + firstNonEmptyCalendarValue(event.Summary, "Calendar event"),
			Body:  summarizePushBody(calendarEventPushBody(event)),
			Data: map[string]string{
				"kind":  "calendar_event_start",
				"start": start.Format(time.RFC3339),
			},
		}); err != nil {
			log.Printf("push scheduler: send upcoming event %q: %v", event.Summary, err)
		}
	}
}

func (p *pushService) send(ctx context.Context, registrations []store.PushRegistration, notification pushNotification) error {
	unique := uniquePushRegistrations(registrations)
	for _, reg := range unique {
		if err := p.gateway.Send(ctx, reg, notification); err != nil {
			return err
		}
	}
	return nil
}

func (p *pushService) markCalendarEventSent(key string, until time.Time) bool {
	if p == nil || strings.TrimSpace(key) == "" {
		return false
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for existing, expiry := range p.sentCalendar {
		if !expiry.After(now) {
			delete(p.sentCalendar, existing)
		}
	}
	if expiry, exists := p.sentCalendar[key]; exists && expiry.After(now) {
		return false
	}
	p.sentCalendar[key] = until
	return true
}

func uniquePushRegistrations(registrations []store.PushRegistration) []store.PushRegistration {
	out := make([]store.PushRegistration, 0, len(registrations))
	seen := map[string]struct{}{}
	for _, reg := range registrations {
		key := strings.ToLower(strings.TrimSpace(reg.Platform)) + "\x00" + strings.TrimSpace(reg.DeviceToken)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, reg)
	}
	return out
}

func summarizePushBody(text string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(stripCanvasFileMarkers(text))), " ")
	if compact == "" {
		return "Open Tabura to view the update."
	}
	runes := []rune(compact)
	if len(runes) <= pushBodyLimit {
		return compact
	}
	return strings.TrimSpace(string(runes[:pushBodyLimit-1])) + "…"
}

func calendarEventPushBody(event calendarEventEntry) string {
	label := event.Start.In(time.Local).Format("15:04")
	summary := firstNonEmptyCalendarValue(event.Summary, "Calendar event")
	if source := strings.TrimSpace(event.Source); source != "" {
		return fmt.Sprintf("%s on %s at %s", summary, source, label)
	}
	return fmt.Sprintf("%s at %s", summary, label)
}

func calendarEventKey(event calendarEventEntry) string {
	return strings.Join([]string{
		strings.TrimSpace(event.Provider),
		strings.TrimSpace(event.Source),
		strings.TrimSpace(event.Summary),
		event.Start.UTC().Format(time.RFC3339),
	}, "|")
}

func (a *App) handlePushRegister(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		SessionID   string `json:"session_id"`
		Platform    string `json:"platform"`
		DeviceToken string `json:"device_token"`
		DeviceLabel string `json:"device_label"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	workspaceID := int64(0)
	if sessionID := strings.TrimSpace(req.SessionID); sessionID != "" {
		session, err := a.store.GetChatSession(sessionID)
		if err != nil {
			writeDomainStoreError(w, err)
			return
		}
		workspaceID = session.WorkspaceID
	}

	registration, err := a.store.UpsertPushRegistration(store.PushRegistration{
		SessionID:   strings.TrimSpace(req.SessionID),
		WorkspaceID: workspaceID,
		Platform:    req.Platform,
		DeviceToken: req.DeviceToken,
		DeviceLabel: req.DeviceLabel,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"registration": registration,
	})
}

func (a *App) maybeNotifyCompletedTurn(sessionID, workspacePath, text string) {
	if a == nil || a.pushService == nil || a.pushService.gateway == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if a.hub != nil && a.hub.hasChatClients(sessionID) {
		return
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		return
	}
	workspaceName := ""
	if workspace, workspaceErr := a.store.GetWorkspaceByStoredPath(workspacePath); workspaceErr == nil {
		workspaceName = workspace.Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushSendTimeout)
	defer cancel()
	if err := a.pushService.notifyTurnCompletion(ctx, sessionID, session.WorkspaceID, workspaceName, text); err != nil {
		log.Printf("push turn notification: %v", err)
	}
}

func (a *App) startPushScheduler() {
	if a == nil || a.shutdownCtx == nil || a.pushService == nil {
		return
	}
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runPushScheduler(a.shutdownCtx)
	}()
}

func (a *App) runPushScheduler(ctx context.Context) {
	if a == nil || a.pushService == nil {
		return
	}
	ticker := time.NewTicker(a.pushService.pollInterval)
	defer ticker.Stop()
	for {
		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		a.pushService.deliverDueCalendarEvents(pollCtx, a)
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
