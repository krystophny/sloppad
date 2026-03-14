package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/turn"
)

type turnWSConn struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	controller *turn.Controller
	sessionID  string
}

func newTurnWSConn(ws *websocket.Conn, sessionID string, profile string, evalLoggingEnabled bool) *turnWSConn {
	conn := &turnWSConn{
		conn:      ws,
		sessionID: strings.TrimSpace(sessionID),
	}
	conn.controller = turn.NewController(turn.Callbacks{
		OnAction: func(signal turn.Signal) {
			metrics := conn.controller.SnapshotMetrics()
			if metrics.EvalLoggingEnabled {
				log.Printf(
					"turn action session=%s profile=%s action=%s reason=%s text=%q rollback_audio_ms=%d playback_active=%t played_audio_ms=%d pending_chars=%d",
					conn.sessionID,
					metrics.Profile,
					signal.Action,
					strings.TrimSpace(signal.Reason),
					strings.TrimSpace(signal.Text),
					signal.RollbackAudioMS,
					metrics.PlaybackActive,
					metrics.PlayedAudioMS,
					metrics.PendingTextChars,
				)
			}
			_ = conn.writeJSON(map[string]any{
				"type":                "turn_action",
				"action":              string(signal.Action),
				"text":                strings.TrimSpace(signal.Text),
				"reason":              strings.TrimSpace(signal.Reason),
				"wait_ms":             signal.WaitMS,
				"interrupt_assistant": signal.InterruptAssistant,
				"rollback_audio_ms":   signal.RollbackAudioMS,
			})
		},
		OnMetrics: func(metrics turn.Metrics) {
			if metrics.EvalLoggingEnabled {
				lastUpdate := ""
				if metrics.Metadata != nil {
					lastUpdate = strings.TrimSpace(toString(metrics.Metadata["last_update"]))
				}
				if lastUpdate == "action" || lastUpdate == "profile" || lastUpdate == "eval_logging" || lastUpdate == "reset" {
					log.Printf(
						"turn metrics session=%s profile=%s last_action=%s last_reason=%s playback_active=%t played_audio_ms=%d speech_starts=%d overlap_yields=%d continuation_timeouts=%d",
						conn.sessionID,
						metrics.Profile,
						strings.TrimSpace(metrics.LastAction),
						strings.TrimSpace(metrics.LastReason),
						metrics.PlaybackActive,
						metrics.PlayedAudioMS,
						metrics.SpeechStarts,
						metrics.SpeechOverlapYields,
						metrics.ContinuationTimeouts,
					)
				}
			}
			_ = conn.writeJSON(map[string]any{
				"type":    "turn_metrics",
				"metrics": metrics,
			})
		},
	}, turn.WithProfile(turn.Profile(profile)), turn.WithEvalLogging(evalLoggingEnabled))
	return conn
}

func shortTurnLogText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 96 {
		return trimmed
	}
	return string(runes[:96]) + "..."
}

func (c *turnWSConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

func (a *App) handleTurnWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newTurnWSConn(ws, sessionID, a.runtimeTurnPolicyProfile(), a.runtimeTurnEvalLoggingEnabled())
	defer func() {
		metrics := conn.controller.SnapshotMetrics()
		if metrics.EvalLoggingEnabled {
			log.Printf(
				"turn session closed session=%s profile=%s last_action=%s last_reason=%s speech_starts=%d overlap_yields=%d playback_interruptions=%d continuation_timeouts=%d",
				sessionID,
				metrics.Profile,
				strings.TrimSpace(metrics.LastAction),
				strings.TrimSpace(metrics.LastReason),
				metrics.SpeechStarts,
				metrics.SpeechOverlapYields,
				metrics.PlaybackInterruptions,
				metrics.ContinuationTimeouts,
			)
		}
		conn.controller.Close()
		_ = ws.Close()
	}()
	metrics := conn.controller.SnapshotMetrics()
	_ = conn.writeJSON(map[string]any{
		"type":                 "turn_ready",
		"session_id":           sessionID,
		"turn_intelligence":    true,
		"turn_endpoint_authed": true,
		"profile":              metrics.Profile,
		"eval_logging_enabled": metrics.EvalLoggingEnabled,
		"metrics":              metrics,
	})
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			continue
		}
		handleTurnWSTextMessage(conn, data)
	}
}

func handleTurnWSTextMessage(conn *turnWSConn, data []byte) {
	if conn == nil || conn.controller == nil {
		return
	}
	var msg struct {
		Type                 string         `json:"type"`
		Kind                 string         `json:"kind"`
		Payload              map[string]any `json:"payload"`
		Active               *bool          `json:"active"`
		Text                 string         `json:"text"`
		DurationMS           int            `json:"duration_ms"`
		InterruptedAssistant bool           `json:"interrupted_assistant"`
		SpeechProb           float64        `json:"speech_prob"`
		Playing              *bool          `json:"playing"`
		PlayedMS             int            `json:"played_ms"`
		Profile              string         `json:"profile"`
		EvalLoggingEnabled   *bool          `json:"eval_logging_enabled"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch strings.TrimSpace(msg.Type) {
	case "turn_reset":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled {
			log.Printf("turn reset session=%s", conn.sessionID)
		}
		conn.controller.Reset()
	case "turn_config":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled {
			log.Printf(
				"turn config session=%s profile=%q eval_logging_enabled=%t",
				conn.sessionID,
				strings.TrimSpace(msg.Profile),
				msg.EvalLoggingEnabled == nil || *msg.EvalLoggingEnabled,
			)
		}
		if strings.TrimSpace(msg.Profile) != "" {
			conn.controller.SetProfile(turn.Profile(msg.Profile))
		}
		if msg.EvalLoggingEnabled != nil {
			conn.controller.SetEvalLogging(*msg.EvalLoggingEnabled)
		}
	case "turn_listen_state":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled && msg.Active != nil {
			log.Printf("turn listen_state session=%s active=%t", conn.sessionID, *msg.Active)
		}
		if msg.Active != nil && !*msg.Active {
			conn.controller.Reset()
		}
	case "turn_speech_start":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled {
			log.Printf(
				"turn speech_start session=%s interrupted_assistant=%t",
				conn.sessionID,
				msg.InterruptedAssistant,
			)
		}
		conn.controller.HandleSpeechStart(msg.InterruptedAssistant)
	case "turn_speech_prob":
		conn.controller.HandleSpeechProbability(msg.SpeechProb, msg.InterruptedAssistant)
	case "turn_transcript_segment":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled {
			log.Printf(
				"turn transcript_segment session=%s chars=%d duration_ms=%d interrupted_assistant=%t text=%q",
				conn.sessionID,
				len([]rune(strings.TrimSpace(msg.Text))),
				msg.DurationMS,
				msg.InterruptedAssistant,
				shortTurnLogText(msg.Text),
			)
		}
		conn.controller.ConsumeSegment(turn.Segment{
			Text:                 msg.Text,
			DurationMS:           msg.DurationMS,
			InterruptedAssistant: msg.InterruptedAssistant,
		})
	case "turn_playback":
		if msg.Playing == nil {
			return
		}
		conn.controller.UpdatePlayback(*msg.Playing, msg.PlayedMS)
	case "turn_client_diag":
		if metrics := conn.controller.SnapshotMetrics(); metrics.EvalLoggingEnabled {
			log.Printf(
				"turn client_diag session=%s kind=%s payload=%s",
				conn.sessionID,
				strings.TrimSpace(msg.Kind),
				compactDialogueDiagnosticPayload(msg.Payload),
			)
		}
	}
}

func toString(value any) string {
	switch t := value.(type) {
	case string:
		return t
	default:
		return ""
	}
}
