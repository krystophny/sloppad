package web

import (
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestEvaluateCompanionDirectedSpeechGate(t *testing.T) {
	cfg := defaultCompanionConfig()
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	session := &store.ParticipantSession{ID: "psess-test", StartedAt: 100}
	events := []store.ParticipantEvent{{EventType: "segment_committed", CreatedAt: 130}}

	t.Run("direct address", func(t *testing.T) {
		segments := []store.ParticipantSegment{{ID: 7, Text: "Tabura, summarize the action items.", CommittedAt: 130}}
		gate := evaluateCompanionDirectedSpeechGate(cfg, session, segments, events)
		if gate.Decision != companionGateDecisionDirect {
			t.Fatalf("decision = %q, want %q", gate.Decision, companionGateDecisionDirect)
		}
		if gate.Reason != "assistant_name_mentioned" {
			t.Fatalf("reason = %q, want assistant_name_mentioned", gate.Reason)
		}
		if gate.SegmentID != 7 {
			t.Fatalf("segment_id = %d, want 7", gate.SegmentID)
		}
	})

	t.Run("non address", func(t *testing.T) {
		segments := []store.ParticipantSegment{{ID: 8, Text: "The budget is blocked until finance signs off.", CommittedAt: 130}}
		gate := evaluateCompanionDirectedSpeechGate(cfg, session, segments, events)
		if gate.Decision != companionGateDecisionNotAddressed {
			t.Fatalf("decision = %q, want %q", gate.Decision, companionGateDecisionNotAddressed)
		}
		if gate.Reason != "no_assistant_address_signal" {
			t.Fatalf("reason = %q, want no_assistant_address_signal", gate.Reason)
		}
	})

	t.Run("uncertain request", func(t *testing.T) {
		segments := []store.ParticipantSegment{{ID: 9, Text: "Can you summarize that?", CommittedAt: 130}}
		gate := evaluateCompanionDirectedSpeechGate(cfg, session, segments, events)
		if gate.Decision != companionGateDecisionUncertain {
			t.Fatalf("decision = %q, want %q", gate.Decision, companionGateDecisionUncertain)
		}
		if gate.Reason != "request_without_assistant_name" {
			t.Fatalf("reason = %q, want request_without_assistant_name", gate.Reason)
		}
	})

	t.Run("target speaker follow up", func(t *testing.T) {
		segments := []store.ParticipantSegment{
			{ID: 10, Speaker: "Alice", Text: "Tabura, summarize that.", CommittedAt: 120},
			{ID: 11, Speaker: "Alice", Text: "Can you send it to the team?", CommittedAt: 130},
		}
		gate := evaluateCompanionDirectedSpeechGate(cfg, session, segments, events)
		if gate.Decision != companionGateDecisionDirect {
			t.Fatalf("decision = %q, want %q", gate.Decision, companionGateDecisionDirect)
		}
		if gate.Reason != "target_speaker_follow_up" {
			t.Fatalf("reason = %q, want target_speaker_follow_up", gate.Reason)
		}
		if gate.TargetSpeaker != "Alice" {
			t.Fatalf("target_speaker = %q, want Alice", gate.TargetSpeaker)
		}
		if !gate.SpeakerMatch {
			t.Fatal("speaker_matched = false, want true")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		disabled := cfg
		disabled.DirectedSpeechGateEnabled = false
		gate := evaluateCompanionDirectedSpeechGate(disabled, session, nil, events)
		if gate.Decision != companionGateDecisionDisabled {
			t.Fatalf("decision = %q, want %q", gate.Decision, companionGateDecisionDisabled)
		}
		if gate.Reason != "gate_disabled" {
			t.Fatalf("reason = %q, want gate_disabled", gate.Reason)
		}
	})
}
