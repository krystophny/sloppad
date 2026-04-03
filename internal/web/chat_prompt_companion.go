package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

const (
	companionPromptEntityLimit          = 8
	companionPromptTopicLimit           = 6
	companionPromptRecentSegmentLimit   = 8
	companionPromptRecentCharLimit      = 1200
	companionPromptSummaryCharLimit     = 480
	companionPromptSegmentTextCharLimit = 240
	companionPromptTriggerLookbackSec   = 10 * 60
	companionPromptTriggerSegmentLimit  = 64
	companionPromptTriggerCharLimit     = 8000
	companionPromptTriggerTextCharLimit = 480
)

type companionPromptSegment struct {
	Speaker string
	At      int64
	Text    string
}

type companionPromptContext struct {
	SessionID        string
	StartedAt        int64
	SummaryText      string
	Entities         []string
	RecentTopics     []string
	RecentTranscript []companionPromptSegment
	OmittedSegments  int
}

func (a *App) loadCompanionPromptContext(workspacePath string) *companionPromptContext {
	if a == nil || a.store == nil {
		return nil
	}
	sessions, err := a.store.ListParticipantSessions(strings.TrimSpace(workspacePath))
	if err != nil {
		return nil
	}
	session, err := selectProjectCompanionSession(sessions, "")
	if err != nil || session == nil {
		return nil
	}

	var memory companionRoomMemory
	if loaded, loadErr := a.loadCompanionRoomMemory(session.ID); loadErr == nil {
		memory = loaded
	}

	segments := []store.ParticipantSegment{}
	if loaded, loadErr := a.store.ListParticipantSegments(session.ID, 0, 0); loadErr == nil {
		segments = loaded
	}

	ctx := buildCompanionPromptContext(session, memory, segments)
	if ctx.empty() {
		return nil
	}
	return ctx
}

func (a *App) loadCompanionPromptContextForTurn(chatSessionID, workspacePath string) *companionPromptContext {
	if a == nil || a.store == nil {
		return nil
	}
	pending, ok := a.companionPendingTurnForChatSession(chatSessionID)
	if !ok || strings.TrimSpace(pending.participantSessionID) == "" {
		return a.loadCompanionPromptContext(workspacePath)
	}
	session, err := a.store.GetParticipantSession(pending.participantSessionID)
	if err != nil || strings.TrimSpace(session.WorkspacePath) != strings.TrimSpace(workspacePath) {
		return a.loadCompanionPromptContext(workspacePath)
	}
	memory, err := a.loadCompanionRoomMemory(session.ID)
	if err != nil {
		return a.loadCompanionPromptContext(workspacePath)
	}
	segments, err := a.store.ListParticipantSegments(session.ID, 0, 0)
	if err != nil {
		return a.loadCompanionPromptContext(workspacePath)
	}
	ctx := buildTriggeredCompanionPromptContext(&session, memory, segments, pending.segmentID)
	if ctx == nil || ctx.empty() {
		return a.loadCompanionPromptContext(workspacePath)
	}
	return ctx
}

func buildCompanionPromptContext(session *store.ParticipantSession, memory companionRoomMemory, segments []store.ParticipantSegment) *companionPromptContext {
	if session == nil {
		return nil
	}
	ctx := &companionPromptContext{
		SessionID:   strings.TrimSpace(session.ID),
		StartedAt:   session.StartedAt,
		SummaryText: truncatePromptValue(memory.SummaryText, companionPromptSummaryCharLimit),
		Entities:    append([]string(nil), firstNonEmptyStrings(memory.Entities, companionPromptEntityLimit)...),
		RecentTopics: append([]string(nil),
			firstNonEmptyStrings(companionPromptTopics(memory.TopicTimeline), companionPromptTopicLimit)...),
	}
	ctx.RecentTranscript, ctx.OmittedSegments = compactCompanionPromptSegments(segments)
	return ctx
}

func buildTriggeredCompanionPromptContext(session *store.ParticipantSession, memory companionRoomMemory, segments []store.ParticipantSegment, triggerSegmentID int64) *companionPromptContext {
	ctx := buildCompanionPromptContext(session, memory, segments)
	if ctx == nil {
		return nil
	}
	ctx.RecentTranscript, ctx.OmittedSegments = compactCompanionPromptSegmentsForTrigger(segments, triggerSegmentID)
	return ctx
}

func (c *companionPromptContext) empty() bool {
	if c == nil {
		return true
	}
	return strings.TrimSpace(c.SummaryText) == "" &&
		len(c.Entities) == 0 &&
		len(c.RecentTopics) == 0 &&
		len(c.RecentTranscript) == 0
}

func appendCompanionPromptContext(b *strings.Builder, ctx *companionPromptContext) {
	if b == nil || ctx == nil || ctx.empty() {
		return
	}
	b.WriteString("## Companion Context\n")
	if ctx.SessionID != "" {
		fmt.Fprintf(b, "- Session: %q", ctx.SessionID)
		if ctx.StartedAt > 0 {
			fmt.Fprintf(b, " (started %s)", time.Unix(ctx.StartedAt, 0).UTC().Format(time.RFC3339))
		}
		b.WriteString("\n")
	}
	if ctx.SummaryText != "" {
		fmt.Fprintf(b, "- Summary: %s\n", ctx.SummaryText)
	}
	if len(ctx.Entities) > 0 {
		fmt.Fprintf(b, "- Entities: %s\n", strings.Join(ctx.Entities, ", "))
	}
	if len(ctx.RecentTopics) > 0 {
		fmt.Fprintf(b, "- Recent topics: %s\n", strings.Join(ctx.RecentTopics, "; "))
	}
	if len(ctx.RecentTranscript) > 0 {
		b.WriteString("- Recent transcript:\n")
		for _, seg := range ctx.RecentTranscript {
			speaker := strings.TrimSpace(seg.Speaker)
			if speaker == "" {
				speaker = "Speaker"
			}
			stamp := "n/a"
			if seg.At > 0 {
				stamp = time.Unix(seg.At, 0).UTC().Format("15:04:05")
			}
			fmt.Fprintf(b, "  - [%s] %s: %s\n", stamp, speaker, seg.Text)
		}
		if ctx.OmittedSegments > 0 {
			fmt.Fprintf(b, "  - Older transcript omitted: %d earlier segments.\n", ctx.OmittedSegments)
		}
	}
	b.WriteString("\n")
}

func compactCompanionPromptSegments(segments []store.ParticipantSegment) ([]companionPromptSegment, int) {
	if len(segments) == 0 {
		return nil, 0
	}
	selected := make([]companionPromptSegment, 0, minInt(len(segments), companionPromptRecentSegmentLimit))
	usedChars := 0
	omitted := 0

	for i := len(segments) - 1; i >= 0; i-- {
		text := truncatePromptValue(segments[i].Text, companionPromptSegmentTextCharLimit)
		if text == "" {
			continue
		}
		segChars := len(text)
		if len(selected) >= companionPromptRecentSegmentLimit || (usedChars+segChars > companionPromptRecentCharLimit && len(selected) > 0) {
			omitted++
			continue
		}
		selected = append(selected, companionPromptSegment{
			Speaker: strings.TrimSpace(segments[i].Speaker),
			At:      maxPromptInt64(segments[i].CommittedAt, maxPromptInt64(segments[i].EndTS, segments[i].StartTS)),
			Text:    text,
		})
		usedChars += segChars
	}

	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected, omitted
}

func compactCompanionPromptSegmentsForTrigger(segments []store.ParticipantSegment, triggerSegmentID int64) ([]companionPromptSegment, int) {
	if len(segments) == 0 {
		return nil, 0
	}
	triggerAt := participantPromptTimestamp(participantSegmentByID(segments, triggerSegmentID))
	if triggerAt == 0 {
		triggerAt = participantPromptTimestamp(&segments[len(segments)-1])
	}
	selected := make([]companionPromptSegment, 0, minInt(len(segments), companionPromptTriggerSegmentLimit))
	usedChars := 0
	omitted := 0

	for i := len(segments) - 1; i >= 0; i-- {
		segment := segments[i]
		text := truncatePromptValue(segment.Text, companionPromptTriggerTextCharLimit)
		if text == "" {
			continue
		}
		at := participantPromptTimestamp(&segment)
		if triggerAt > 0 && at > 0 && segment.ID != triggerSegmentID && at < triggerAt-companionPromptTriggerLookbackSec {
			omitted++
			continue
		}
		segChars := len(text)
		if len(selected) >= companionPromptTriggerSegmentLimit || (usedChars+segChars > companionPromptTriggerCharLimit && len(selected) > 0 && segment.ID != triggerSegmentID) {
			omitted++
			continue
		}
		selected = append(selected, companionPromptSegment{
			Speaker: strings.TrimSpace(segment.Speaker),
			At:      at,
			Text:    text,
		})
		usedChars += segChars
	}

	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected, omitted
}

func participantPromptTimestamp(segment *store.ParticipantSegment) int64 {
	if segment == nil {
		return 0
	}
	return maxPromptInt64(segment.CommittedAt, maxPromptInt64(segment.EndTS, segment.StartTS))
}

func companionPromptTopics(items []any) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for i := len(items) - 1; i >= 0 && len(out) < companionPromptTopicLimit; i-- {
		value := truncatePromptValue(formatCompanionTopicTimelineItem(items[i]), companionPromptSegmentTextCharLimit)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func firstNonEmptyStrings(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	out := make([]string, 0, minInt(len(values), limit))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
		if len(out) == limit {
			break
		}
	}
	return out
}

func truncatePromptValue(value string, limit int) string {
	clean := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if clean == "" || limit <= 0 || len(clean) <= limit {
		return clean
	}
	if limit <= 3 {
		return clean[:limit]
	}
	return strings.TrimSpace(clean[:limit-3]) + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxPromptInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
