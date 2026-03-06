package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestBuildPromptFromHistoryForModeWithCompanionCompactsContext(t *testing.T) {
	session := &store.ParticipantSession{ID: "psess-1", StartedAt: 1710000000}
	memory := companionRoomMemory{
		SummaryText: "Budget review remains blocked on owner confirmation for the Acme Cloud rollout.",
		Entities:    []string{"Acme Cloud", "Budget", "Alice", "Budget"},
		TopicTimeline: []any{
			map[string]any{"topic": "Kickoff"},
			map[string]any{"topic": "Budget review"},
			map[string]any{"topic": "Owner follow-up"},
		},
	}
	segments := make([]store.ParticipantSegment, 0, 10)
	for i := 0; i < 10; i++ {
		segments = append(segments, store.ParticipantSegment{
			SessionID:   session.ID,
			StartTS:     int64(100 + i),
			CommittedAt: int64(100 + i),
			Speaker:     "Alice",
			Text:        fmt.Sprintf("segment-%d %s", i, strings.Repeat("detail ", 40)),
			Status:      "final",
		})
	}

	ctx := buildCompanionPromptContext(session, memory, segments)
	prompt := buildPromptFromHistoryForModeWithCompanion("chat", []store.ChatMessage{{
		Role:         "user",
		ContentPlain: "What changed?",
	}}, nil, ctx, turnOutputModeVoice, "")

	if !strings.Contains(prompt, "## Companion Context") {
		t.Fatal("prompt should include companion context section")
	}
	if !strings.Contains(prompt, "Summary: Budget review remains blocked on owner confirmation") {
		t.Fatalf("prompt missing summary: %q", prompt)
	}
	if !strings.Contains(prompt, "Entities: Acme Cloud, Budget, Alice") {
		t.Fatalf("prompt missing entities: %q", prompt)
	}
	if !strings.Contains(prompt, "Recent topics: Kickoff; Budget review; Owner follow-up") {
		t.Fatalf("prompt missing recent topics: %q", prompt)
	}
	if !strings.Contains(prompt, "segment-9") {
		t.Fatalf("prompt missing newest segment: %q", prompt)
	}
	if strings.Contains(prompt, "segment-0") {
		t.Fatalf("prompt should omit oldest compacted segment: %q", prompt)
	}
	if !strings.Contains(prompt, "Older transcript omitted:") {
		t.Fatalf("prompt should report omitted transcript segments: %q", prompt)
	}
	if !strings.Contains(prompt, "Conversation transcript:\nUSER:\nWhat changed?") {
		t.Fatalf("prompt missing conversation transcript: %q", prompt)
	}
}
