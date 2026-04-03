package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

const (
	meetingTemplatePathEnv          = "SLOPSHELL_MEETING_TEMPLATE_PATH"
	meetingSummaryTemplateDefault   = "# Meeting Notes\n\n- **Date/Time:** ...\n- **Transcript Language:** ...\n- **Confidence Note:** ...\n- **Participants:** short list of the most likely participants by name only; keep uncertainty explicit when needed\n\n## Decisions\n\n- short bullets for explicit decisions only\n\n## Action Checklist\n\n### Person Name\n\n- [ ] actionable follow-up item\n\n### General / Unassigned\n\n- [ ] use only if ownership cannot be inferred\n\n## Open Questions / Risks\n\n- unresolved issues, uncertainties, or risks\n\n## Topics and Outcomes\n\n- detailed meeting flow\n- supporting context\n- lower-priority detail that belongs after participants, decisions, checklist, and open points\n\n## Participant Context\n\n- roles\n- attendance uncertainty notes\n- any name disambiguation that belongs below the action-focused summary\n"
	meetingSummaryConfidenceDefault = "Derived from live transcript segments. Names, ownership, and inferred outcomes may need confirmation."
	meetingSummaryArtifactTitle     = "Meeting Summary"
)

type finalizeMeetingRequest struct {
	DiscardTranscript bool `json:"discard_transcript"`
}

type finalizeMeetingResponse struct {
	OK                  bool   `json:"ok"`
	WorkspaceID         string `json:"workspace_id"`
	WorkspacePath       string `json:"workspace_path"`
	SessionID           string `json:"session_id"`
	SummaryText         string `json:"summary_text"`
	SummaryArtifactID   int64  `json:"summary_artifact_id"`
	TranscriptDiscarded bool   `json:"transcript_discarded"`
}

func meetingSummaryTemplate() string {
	overridePath := strings.TrimSpace(os.Getenv(meetingTemplatePathEnv))
	if overridePath == "" {
		return meetingSummaryTemplateDefault
	}
	body, err := os.ReadFile(filepath.Clean(overridePath))
	if err != nil {
		return meetingSummaryTemplateDefault
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return meetingSummaryTemplateDefault
	}
	return text
}

func dedupeMeetingEntities(primary, secondary []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(primary)+len(secondary))
	for _, raw := range append(primary, secondary...) {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func meetingOpenQuestions(segments []store.ParticipantSegment) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	for _, seg := range segments {
		text := strings.TrimSpace(strings.Join(strings.Fields(seg.Text), " "))
		if text == "" || isCompanionDirectAddress(text) || !strings.Contains(text, "?") {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
		if len(out) == 6 {
			break
		}
	}
	return out
}

func sanitizeMeetingTopicTimeline(notes meetingNotesSnapshot) []map[string]any {
	out := make([]map[string]any, 0, len(notes.Decisions)+len(notes.ActionItems)+1)
	for _, decision := range notes.Decisions {
		clean := strings.TrimSpace(decision)
		if clean == "" {
			continue
		}
		out = append(out, map[string]any{
			"topic":  "Decision",
			"detail": clean,
		})
	}
	for _, item := range notes.ActionItems {
		title := strings.TrimSpace(item.ItemTitle)
		if title == "" {
			continue
		}
		actor := strings.TrimSpace(item.ActorName)
		if actor == "" {
			actor = "General / Unassigned"
		}
		out = append(out, map[string]any{
			"topic":  "Action item",
			"detail": actor + ": " + title,
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{
			"topic":  "Meeting summary",
			"detail": "Captured during the meeting summary pass.",
		})
	}
	return out
}

func renderMeetingSummaryMarkdown(session *store.ParticipantSession, cfg companionConfig, notes meetingNotesSnapshot, memory companionRoomMemory, segments []store.ParticipantSegment) string {
	started := "n/a"
	if session != nil && session.StartedAt > 0 {
		started = time.Unix(session.StartedAt, 0).Format(time.RFC3339)
	}
	language := strings.TrimSpace(cfg.Language)
	if language == "" {
		language = "unknown"
	}
	participants := notes.Participants
	if len(participants) == 0 {
		participants = dedupeMeetingEntities(nil, memory.Entities)
	}
	if len(participants) == 0 {
		participants = []string{"Unknown"}
	}
	openQuestions := meetingOpenQuestions(segments)

	var b strings.Builder
	b.WriteString("# Meeting Notes\n\n")
	fmt.Fprintf(&b, "- **Date/Time:** %s\n", started)
	fmt.Fprintf(&b, "- **Transcript Language:** %s\n", language)
	fmt.Fprintf(&b, "- **Confidence Note:** %s\n", meetingSummaryConfidenceDefault)
	fmt.Fprintf(&b, "- **Participants:** %s\n", strings.Join(participants, ", "))

	b.WriteString("\n## Decisions\n\n")
	if len(notes.Decisions) == 0 {
		b.WriteString("- none captured\n")
	} else {
		for _, decision := range notes.Decisions {
			fmt.Fprintf(&b, "- %s\n", decision)
		}
	}

	b.WriteString("\n## Action Checklist\n\n")
	grouped := map[string][]string{}
	order := make([]string, 0, len(notes.ActionItems))
	for _, item := range notes.ActionItems {
		title := strings.TrimSpace(item.ItemTitle)
		if title == "" {
			continue
		}
		actor := strings.TrimSpace(item.ActorName)
		if actor == "" {
			actor = "General / Unassigned"
		}
		if _, ok := grouped[actor]; !ok {
			order = append(order, actor)
		}
		grouped[actor] = append(grouped[actor], title)
	}
	if len(order) == 0 {
		b.WriteString("### General / Unassigned\n\n- [ ] none captured\n")
	} else {
		for _, actor := range order {
			b.WriteString("### ")
			b.WriteString(actor)
			b.WriteString("\n\n")
			for _, title := range grouped[actor] {
				fmt.Fprintf(&b, "- [ ] %s\n", title)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Open Questions / Risks\n\n")
	if len(openQuestions) == 0 {
		b.WriteString("- none captured\n")
	} else {
		for _, question := range openQuestions {
			fmt.Fprintf(&b, "- %s\n", question)
		}
	}

	b.WriteString("\n## Topics and Outcomes\n\n")
	if len(notes.KeyTopics) == 0 && strings.TrimSpace(memory.SummaryText) == "" {
		b.WriteString("- no topic summary captured\n")
	} else {
		for _, topic := range notes.KeyTopics {
			fmt.Fprintf(&b, "- %s\n", topic)
		}
		if summary := strings.TrimSpace(memory.SummaryText); summary != "" {
			fmt.Fprintf(&b, "- Summary outcome: %s\n", summary)
		}
	}

	b.WriteString("\n## Participant Context\n\n")
	for _, participant := range participants {
		fmt.Fprintf(&b, "- %s\n", participant)
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func (a *App) upsertMeetingSummaryArtifact(workspace store.Workspace, project *store.Workspace, session *store.ParticipantSession, summaryText string) (store.Artifact, error) {
	if a == nil || a.store == nil {
		return store.Artifact{}, fmt.Errorf("store is not configured")
	}
	if session == nil {
		return store.Artifact{}, fmt.Errorf("meeting session is required")
	}
	summaryPath := filepath.Join(companionArtifactDir(workspace, session), "summary.md")
	workspaceID, workspacePath := meetingPayloadProject(workspace, project)
	metaPayload := map[string]any{
		"source":         meetingSummaryItemSource,
		"summary":        strings.TrimSpace(summaryText),
		"session_id":     session.ID,
		"workspace_id":   workspaceID,
		"workspace_path": workspacePath,
	}
	raw, err := json.Marshal(metaPayload)
	if err != nil {
		return store.Artifact{}, err
	}
	metaJSON := string(raw)
	artifacts, err := a.store.ListArtifactsByKind(store.ArtifactKindMarkdown)
	if err != nil {
		return store.Artifact{}, err
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(optionalStringValue(artifact.RefPath)) != summaryPath {
			continue
		}
		title := meetingSummaryArtifactTitle
		if err := a.store.UpdateArtifact(artifact.ID, store.ArtifactUpdate{
			RefPath:  &summaryPath,
			Title:    &title,
			MetaJSON: &metaJSON,
		}); err != nil {
			return store.Artifact{}, err
		}
		return a.store.GetArtifact(artifact.ID)
	}
	title := meetingSummaryArtifactTitle
	return a.store.CreateArtifact(store.ArtifactKindMarkdown, &summaryPath, nil, &title, &metaJSON)
}

func (a *App) finalizeMeetingSession(workspace store.Workspace, project *store.Workspace, session *store.ParticipantSession, discardTranscript bool) (finalizeMeetingResponse, error) {
	if a == nil || a.store == nil {
		return finalizeMeetingResponse{}, fmt.Errorf("store is not configured")
	}
	if session == nil {
		return finalizeMeetingResponse{}, fmt.Errorf("meeting session is required")
	}
	if strings.TrimSpace(meetingSummaryTemplate()) == "" {
		return finalizeMeetingResponse{}, fmt.Errorf("meeting summary template is empty")
	}
	segments, err := a.store.ListParticipantSegments(session.ID, 0, 0)
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	events, err := a.store.ListParticipantEvents(session.ID)
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	memory, err := a.loadCompanionRoomMemory(session.ID)
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	notes := buildMeetingNotesSnapshot(segments, events, memory)
	cfg := defaultCompanionConfig()
	if project != nil {
		cfg = a.loadCompanionConfig(project)
	} else {
		cfg = a.loadCompanionConfig(workspace)
	}
	summaryText := renderMeetingSummaryMarkdown(session, cfg, notes, memory, segments)

	entities := dedupeMeetingEntities(notes.Participants, memory.Entities)
	entitiesJSONBytes, err := json.Marshal(entities)
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	topicTimelineJSONBytes, err := json.Marshal(sanitizeMeetingTopicTimeline(notes))
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	if err := a.store.UpsertParticipantRoomState(session.ID, summaryText, string(entitiesJSONBytes), string(topicTimelineJSONBytes)); err != nil {
		return finalizeMeetingResponse{}, err
	}

	summaryArtifact, err := a.upsertMeetingSummaryArtifact(workspace, project, session, summaryText)
	if err != nil {
		return finalizeMeetingResponse{}, err
	}
	summaryPath := filepath.Join(companionArtifactDir(workspace, session), "summary.md")
	if err := writeCompanionArtifactFile(summaryPath, renderCompanionSummaryMarkdown(session, summaryText, time.Now().Unix(), notes)); err != nil {
		return finalizeMeetingResponse{}, err
	}

	if session.EndedAt == 0 {
		if err := a.store.EndParticipantSession(session.ID); err != nil {
			return finalizeMeetingResponse{}, err
		}
	}
	if discardTranscript {
		if err := a.store.DeleteParticipantSegments(session.ID); err != nil {
			return finalizeMeetingResponse{}, err
		}
		if err := a.store.DeleteParticipantEvents(session.ID); err != nil {
			return finalizeMeetingResponse{}, err
		}
		dir := companionArtifactDir(workspace, session)
		_ = os.Remove(filepath.Join(dir, "transcript.md"))
		_ = os.Remove(filepath.Join(dir, "references.md"))
	}

	workspaceID, workspacePath := meetingPayloadProject(workspace, project)
	return finalizeMeetingResponse{
		OK:                  true,
		WorkspaceID:         workspaceID,
		WorkspacePath:       workspacePath,
		SessionID:           session.ID,
		SummaryText:         summaryText,
		SummaryArtifactID:   summaryArtifact.ID,
		TranscriptDiscarded: discardTranscript,
	}, nil
}

func (a *App) handleWorkspaceMeetingFinalize(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, project, _, session, ok := a.resolveWorkspaceCompanionArtifact(w, r)
	if !ok {
		return
	}
	if session == nil {
		http.Error(w, "meeting session not available", http.StatusBadRequest)
		return
	}
	req := finalizeMeetingRequest{DiscardTranscript: true}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	resp, err := a.finalizeMeetingSession(workspace, project, session, req.DiscardTranscript)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}
