package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestLooksLikeSilentLiveEditIntent(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "english rewrite", text: "Let's make this point more precise.", want: true},
		{name: "english add", text: "Add one more sentence at the bottom.", want: true},
		{name: "german rewrite", text: "Ändern wir das besser auf eine präzisere Formulierung.", want: true},
		{name: "german add", text: "Fügen wir hier noch einen Punkt hinzu.", want: true},
		{name: "question stays false", text: "Can you explain this paragraph?", want: false},
		{name: "plain statement stays false", text: "This paragraph is important.", want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeSilentLiveEditIntent(tc.text); got != tc.want {
				t.Fatalf("looksLikeSilentLiveEditIntent(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestShouldAttemptSilentLiveEdit(t *testing.T) {
	tests := []struct {
		name        string
		policy      LivePolicy
		captureMode string
		text        string
		want        bool
	}{
		{name: "dialogue edit", policy: LivePolicyDialogue, captureMode: chatCaptureModeVoice, text: "Let's rewrite this paragraph.", want: true},
		{name: "meeting unaddressed edit", policy: LivePolicyMeeting, captureMode: chatCaptureModeVoice, text: "Fügen wir hier noch einen Satz hinzu.", want: true},
		{name: "meeting addressed companion", policy: LivePolicyMeeting, captureMode: chatCaptureModeVoice, text: "Computer, rewrite this paragraph.", want: false},
		{name: "text never silent edit", policy: LivePolicyDialogue, captureMode: chatCaptureModeText, text: "Rewrite this paragraph.", want: false},
		{name: "non edit text", policy: LivePolicyDialogue, captureMode: chatCaptureModeVoice, text: "Tell me what this means.", want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAttemptSilentLiveEdit(tc.policy, tc.captureMode, tc.text); got != tc.want {
				t.Fatalf("shouldAttemptSilentLiveEdit(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestCompactSilentLiveEditDocumentPreservesFocusWindow(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1800; i++ {
		b.WriteString("alpha beta gamma delta epsilon zeta eta theta\n")
	}
	b.WriteString("FOCUS SENTENCE THAT MUST SURVIVE\n")
	for i := 0; i < 1800; i++ {
		b.WriteString("iota kappa lambda mu nu xi omicron pi\n")
	}
	document := b.String()

	compacted, changed := compactSilentLiveEditDocument(document, &chatCursorContext{
		SelectedText: "FOCUS SENTENCE THAT MUST SURVIVE",
	})
	if !changed {
		t.Fatal("expected document compaction")
	}
	if !strings.Contains(compacted, "FOCUS SENTENCE THAT MUST SURVIVE") {
		t.Fatalf("compacted document missing selected focus text:\n%s", compacted)
	}
	if !strings.Contains(compacted, "[[document_compact omitted_chars=") {
		t.Fatalf("compacted document missing omission marker:\n%s", compacted)
	}
}

func TestMeetingDocumentFollowHeuristicAction(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		wantAction string
		wantReason string
	}{
		{name: "english next", transcript: "On the next slide we compare the baselines.", wantAction: "next", wantReason: "explicit_next_phrase"},
		{name: "german previous", transcript: "Gehen wir noch einmal zurück.", wantAction: "previous", wantReason: "explicit_previous_phrase"},
		{name: "stay", transcript: "Here we discuss the current slide.", wantAction: "stay", wantReason: ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			action, reason := meetingDocumentFollowHeuristicAction(meetingDocumentFollowRequest{
				Transcript: tc.transcript,
			})
			if action != tc.wantAction || reason != tc.wantReason {
				t.Fatalf("meetingDocumentFollowHeuristicAction(%q) = (%q, %q), want (%q, %q)", tc.transcript, action, reason, tc.wantAction, tc.wantReason)
			}
		})
	}
}

func TestHandleMeetingDocumentFollowDecideUsesHeuristicAndValidation(t *testing.T) {
	app := newAuthedTestApp(t)
	router := app.Router()

	rrMissing := doAuthedJSONRequest(t, router, http.MethodPost, "/api/participant/document-follow/decide", map[string]any{
		"transcript": "next slide",
	})
	if rrMissing.Code != http.StatusOK {
		t.Fatalf("missing current status = %d, want 200", rrMissing.Code)
	}
	payloadMissing := decodeJSONResponse(t, rrMissing)
	if got := strFromAny(payloadMissing["action"]); got != "stay" {
		t.Fatalf("missing current action = %q, want stay", got)
	}
	if got := strFromAny(payloadMissing["source"]); got != "validation" {
		t.Fatalf("missing current source = %q, want validation", got)
	}

	rrNext := doAuthedJSONRequest(t, router, http.MethodPost, "/api/participant/document-follow/decide", map[string]any{
		"transcript":     "Wenn wir jetzt zur nächsten Folie gehen",
		"artifact_kind":  "pdf_artifact",
		"artifact_title": "demo.pdf",
		"current": map[string]any{
			"id":    "pdf-page-1",
			"label": "Page 1 / 6",
			"text":  "Introduction",
		},
		"next": map[string]any{
			"id":    "pdf-page-2",
			"label": "Page 2 / 6",
			"text":  "Results",
		},
	})
	if rrNext.Code != http.StatusOK {
		t.Fatalf("heuristic next status = %d, want 200", rrNext.Code)
	}
	payloadNext := decodeJSONResponse(t, rrNext)
	if got := strFromAny(payloadNext["action"]); got != "next" {
		t.Fatalf("heuristic next action = %q, want next", got)
	}
	if got := strFromAny(payloadNext["source"]); got != "heuristic" {
		t.Fatalf("heuristic next source = %q, want heuristic", got)
	}
}

func TestBuildParticipantTranscriptEntriesIncludesDocumentPositionEvents(t *testing.T) {
	payloadJSON, err := json.Marshal(participantDocumentPositionPayload{
		Gesture:       "document_flip",
		ArtifactTitle: "deck.pdf",
		ArtifactPath:  "deck.pdf",
		View:          "pdf",
		Element:       "page",
		Page:          3,
		RelativeX:     0.5,
		RelativeY:     0.5,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	entries := buildParticipantTranscriptEntries(
		[]store.ParticipantSegment{
			{
				ID:      17,
				StartTS: 200,
				EndTS:   240,
				Speaker: "Presenter",
				Text:    "Now we compare the baselines.",
			},
		},
		[]store.ParticipantEvent{
			{
				ID:          9,
				EventType:   participantDocumentPositionEventType,
				CreatedAt:   180,
				PayloadJSON: string(payloadJSON),
			},
		},
	)
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries[0].Kind != "document_position" {
		t.Fatalf("entries[0].Kind = %q, want document_position", entries[0].Kind)
	}
	if entries[0].Document == nil || entries[0].Document.Page != 3 {
		t.Fatalf("entries[0].Document = %#v, want page 3", entries[0].Document)
	}
	if entries[1].Kind != "segment" {
		t.Fatalf("entries[1].Kind = %q, want segment", entries[1].Kind)
	}
	description := describeParticipantDocumentPosition(entries[0].Document)
	if !strings.Contains(description, "deck.pdf") || !strings.Contains(description, "page 3") {
		t.Fatalf("describeParticipantDocumentPosition() = %q, want deck/page details", description)
	}
}
