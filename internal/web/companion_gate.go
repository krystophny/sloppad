package web

import (
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

const (
	companionGateDecisionDisabled     = "disabled"
	companionGateDecisionDirect       = "direct_address"
	companionGateDecisionNotAddressed = "not_addressed"
	companionGateDecisionUncertain    = "uncertain"
)

type companionDirectedSpeechGate struct {
	Enabled       bool   `json:"enabled"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SessionID     string `json:"session_id,omitempty"`
	SegmentID     int64  `json:"segment_id,omitempty"`
	Speaker       string `json:"speaker,omitempty"`
	TargetSpeaker string `json:"target_speaker,omitempty"`
	TargetSegment int64  `json:"target_segment_id,omitempty"`
	SpeakerMatch  bool   `json:"speaker_matched,omitempty"`
	EvaluatedText string `json:"evaluated_text,omitempty"`
	EvaluatedAt   int64  `json:"evaluated_at,omitempty"`
	LastEventType string `json:"last_event_type,omitempty"`
}

var (
	companionAddressLeadPattern = regexp.MustCompile(`(?i)^(?:hey|ok|okay)\s+(?:tabura|assistant)\b|^(?:tabura|assistant)\b`)
	companionAddressCuePattern  = regexp.MustCompile(`(?i)\b(?:tabura|assistant)\b[:,!?]`)
	companionRequestPattern     = regexp.MustCompile(`(?i)\b(?:can|could|would|will)\s+you\b|^(?:please\s+)?(?:summarize|open|show|tell|give|find|write|draft|explain|list|track|remind|create|help)\b|^(?:what|when|where|why|how)\b`)
)

func (a *App) loadCompanionDirectedSpeechGate(cfg companionConfig, session *store.ParticipantSession) companionDirectedSpeechGate {
	if !cfg.CompanionEnabled || !cfg.DirectedSpeechGateEnabled {
		return evaluateCompanionDirectedSpeechGate(cfg, session, nil, nil)
	}
	if session == nil {
		return evaluateCompanionDirectedSpeechGate(cfg, nil, nil, nil)
	}
	segments, err := a.store.ListParticipantSegments(session.ID, 0, 0)
	if err != nil {
		return companionDirectedSpeechGate{
			Enabled:   cfg.DirectedSpeechGateEnabled,
			Decision:  companionGateDecisionUncertain,
			Reason:    "segment_lookup_failed",
			SessionID: session.ID,
		}
	}
	events, err := a.store.ListParticipantEvents(session.ID)
	if err != nil {
		return companionDirectedSpeechGate{
			Enabled:   cfg.DirectedSpeechGateEnabled,
			Decision:  companionGateDecisionUncertain,
			Reason:    "event_lookup_failed",
			SessionID: session.ID,
		}
	}
	return evaluateCompanionDirectedSpeechGate(cfg, session, segments, events)
}

func evaluateCompanionDirectedSpeechGate(cfg companionConfig, session *store.ParticipantSession, segments []store.ParticipantSegment, events []store.ParticipantEvent) companionDirectedSpeechGate {
	gate := companionDirectedSpeechGate{
		Enabled:  cfg.DirectedSpeechGateEnabled,
		Decision: companionGateDecisionUncertain,
		Reason:   "no_transcript_context",
	}
	if !cfg.CompanionEnabled {
		gate.Decision = companionGateDecisionDisabled
		gate.Reason = "companion_disabled"
		return gate
	}
	if !cfg.DirectedSpeechGateEnabled {
		gate.Decision = companionGateDecisionDisabled
		gate.Reason = "gate_disabled"
		return gate
	}
	if session == nil {
		return gate
	}
	gate.SessionID = session.ID
	if len(events) > 0 {
		lastEvent := events[len(events)-1]
		gate.LastEventType = lastEvent.EventType
		gate.EvaluatedAt = lastEvent.CreatedAt
	}
	latest := latestMeaningfulParticipantSegment(segments)
	if latest == nil {
		if gate.LastEventType == "session_started" {
			gate.Reason = "awaiting_transcript"
		}
		return gate
	}
	gate.SegmentID = latest.ID
	gate.Speaker = normalizeCompanionSpeaker(latest.Speaker)
	gate.EvaluatedText = strings.TrimSpace(latest.Text)
	if latest.CommittedAt > 0 {
		gate.EvaluatedAt = latest.CommittedAt
	}
	gate.TargetSpeaker, gate.TargetSegment = companionTargetSpeaker(segments, events)
	gate.SpeakerMatch = companionSpeakersMatch(gate.Speaker, gate.TargetSpeaker)
	if isCompanionDirectAddress(latest.Text) {
		gate.Decision = companionGateDecisionDirect
		gate.Reason = "assistant_name_mentioned"
		return gate
	}
	if gate.SpeakerMatch && isCompanionRequestWithoutDirectAddress(latest.Text) {
		gate.Decision = companionGateDecisionDirect
		gate.Reason = "target_speaker_follow_up"
		return gate
	}
	if isCompanionRequestWithoutDirectAddress(latest.Text) {
		gate.Reason = "request_without_assistant_name"
		return gate
	}
	gate.Decision = companionGateDecisionNotAddressed
	gate.Reason = "no_assistant_address_signal"
	return gate
}

func latestMeaningfulParticipantSegment(segments []store.ParticipantSegment) *store.ParticipantSegment {
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.TrimSpace(segments[i].Text) == "" {
			continue
		}
		return &segments[i]
	}
	return nil
}

func latestDirectAddressedParticipantSegment(segments []store.ParticipantSegment) *store.ParticipantSegment {
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.TrimSpace(segments[i].Text) == "" {
			continue
		}
		if isCompanionDirectAddress(segments[i].Text) {
			return &segments[i]
		}
	}
	return nil
}

func participantSegmentByID(segments []store.ParticipantSegment, segmentID int64) *store.ParticipantSegment {
	if segmentID == 0 {
		return nil
	}
	for i := range segments {
		if segments[i].ID == segmentID {
			return &segments[i]
		}
	}
	return nil
}

func companionTargetSpeaker(segments []store.ParticipantSegment, events []store.ParticipantEvent) (string, int64) {
	if pendingSegmentID, _ := latestPendingCompanionSegment(events); pendingSegmentID != 0 {
		if segment := participantSegmentByID(segments, pendingSegmentID); segment != nil {
			if speaker := normalizeCompanionSpeaker(segment.Speaker); speaker != "" {
				return speaker, segment.ID
			}
		}
	}
	if latest := latestDirectAddressedParticipantSegment(segments); latest != nil {
		if speaker := normalizeCompanionSpeaker(latest.Speaker); speaker != "" {
			return speaker, latest.ID
		}
	}
	return "", 0
}

func normalizeCompanionSpeaker(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

func companionSpeakersMatch(a, b string) bool {
	left := normalizeCompanionSpeaker(a)
	right := normalizeCompanionSpeaker(b)
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func isCompanionDirectAddress(raw string) bool {
	text := normalizeCompanionGateText(raw)
	if text == "" {
		return false
	}
	if companionAddressLeadPattern.MatchString(text) || companionAddressCuePattern.MatchString(text) {
		return true
	}
	return companionRequestPattern.MatchString(text) && strings.Contains(text, "tabura")
}

func isCompanionRequestWithoutDirectAddress(raw string) bool {
	text := normalizeCompanionGateText(raw)
	if text == "" || isCompanionDirectAddress(text) {
		return false
	}
	return companionRequestPattern.MatchString(text)
}

func normalizeCompanionGateText(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}
