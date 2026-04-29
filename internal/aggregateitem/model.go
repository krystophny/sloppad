package aggregateitem

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SourceKind string

const (
	SourceKindMarkdown SourceKind = "markdown"
	SourceKindTodoist  SourceKind = "todoist"
	SourceKindGitHub   SourceKind = "github"
	SourceKindGitLab   SourceKind = "gitlab"
	SourceKindEmail    SourceKind = "email"
	SourceKindLocal    SourceKind = "local"

	StateInbox   = "inbox"
	StateWaiting = "waiting"
	StateSomeday = "someday"
	StateDone    = "done"
)

type AggregateItem struct {
	ID         string          `json:"id"`
	Bindings   []SourceBinding `json:"bindings"`
	Source     SourceFields    `json:"source"`
	Overlay    LocalOverlay    `json:"overlay"`
	Projection GTDProjection   `json:"projection"`
}

type SourceBinding struct {
	Kind            SourceKind       `json:"kind"`
	Provider        string           `json:"provider,omitempty"`
	AccountID       *int64           `json:"account_id,omitempty"`
	ObjectType      string           `json:"object_type,omitempty"`
	RemoteID        string           `json:"remote_id,omitempty"`
	SourceRef       string           `json:"source_ref,omitempty"`
	ContainerRef    *string          `json:"container_ref,omitempty"`
	URL             *string          `json:"url,omitempty"`
	RemoteUpdatedAt *time.Time       `json:"remote_updated_at,omitempty"`
	Authority       BackendAuthority `json:"authority"`
}

type BackendAuthority struct {
	Backend            string   `json:"backend"`
	SourceFields       []string `json:"source_fields,omitempty"`
	LocalOverlayFields []string `json:"local_overlay_fields,omitempty"`
}

type SourceFields struct {
	Title     string     `json:"title,omitempty"`
	Body      string     `json:"body,omitempty"`
	State     string     `json:"state,omitempty"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	Labels    []string   `json:"labels,omitempty"`
	URL       *string    `json:"url,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type LocalOverlay struct {
	Title        *string    `json:"title,omitempty"`
	State        *string    `json:"state,omitempty"`
	WorkspaceID  *int64     `json:"workspace_id,omitempty"`
	Sphere       *string    `json:"sphere,omitempty"`
	ArtifactID   *int64     `json:"artifact_id,omitempty"`
	ActorID      *int64     `json:"actor_id,omitempty"`
	VisibleAfter *time.Time `json:"visible_after,omitempty"`
	FollowUpAt   *time.Time `json:"follow_up_at,omitempty"`
	Labels       []string   `json:"labels,omitempty"`
	ReviewTarget *string    `json:"review_target,omitempty"`
	Reviewer     *string    `json:"reviewer,omitempty"`
}

type GTDProjection struct {
	Title        string       `json:"title"`
	State        string       `json:"state"`
	Sphere       string       `json:"sphere,omitempty"`
	WorkspaceID  *int64       `json:"workspace_id,omitempty"`
	ArtifactID   *int64       `json:"artifact_id,omitempty"`
	ActorID      *int64       `json:"actor_id,omitempty"`
	SourceKinds  []SourceKind `json:"source_kinds,omitempty"`
	Providers    []string     `json:"providers,omitempty"`
	Labels       []string     `json:"labels,omitempty"`
	DueAt        *time.Time   `json:"due_at,omitempty"`
	VisibleAfter *time.Time   `json:"visible_after,omitempty"`
	FollowUpAt   *time.Time   `json:"follow_up_at,omitempty"`
	ReviewTarget *string      `json:"review_target,omitempty"`
	Reviewer     *string      `json:"reviewer,omitempty"`
}

func New(id string, bindings []SourceBinding, source SourceFields, overlay LocalOverlay) (AggregateItem, error) {
	item := AggregateItem{
		ID:       strings.TrimSpace(id),
		Bindings: NormalizeBindings(bindings),
		Source:   normalizeSourceFields(source),
		Overlay:  normalizeLocalOverlay(overlay),
	}
	if err := item.Validate(); err != nil {
		return AggregateItem{}, err
	}
	item.Projection = BuildProjection(item.Bindings, item.Source, item.Overlay)
	return item, nil
}

func (i AggregateItem) Validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return errors.New("aggregate item id is required")
	}
	if len(i.Bindings) == 0 {
		return errors.New("aggregate item requires at least one source binding")
	}
	for index, binding := range i.Bindings {
		if err := binding.Validate(); err != nil {
			return fmt.Errorf("binding %d: %w", index, err)
		}
	}
	return nil
}

func NormalizeBindings(bindings []SourceBinding) []SourceBinding {
	out := make([]SourceBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, normalizeBinding(binding))
	}
	return out
}

func (b SourceBinding) Validate() error {
	switch b.Kind {
	case SourceKindMarkdown, SourceKindLocal:
		if strings.TrimSpace(b.SourceRef) == "" {
			return errors.New("source_ref is required")
		}
	case SourceKindTodoist, SourceKindGitHub, SourceKindGitLab, SourceKindEmail:
		if strings.TrimSpace(b.Provider) == "" {
			return errors.New("provider is required")
		}
		if strings.TrimSpace(b.ObjectType) == "" {
			return errors.New("object_type is required")
		}
		if strings.TrimSpace(b.RemoteID) == "" {
			return errors.New("remote_id is required")
		}
	default:
		return fmt.Errorf("unsupported source kind %q", b.Kind)
	}
	if strings.TrimSpace(b.Authority.Backend) == "" {
		return errors.New("authority backend is required")
	}
	return nil
}

func BuildProjection(bindings []SourceBinding, source SourceFields, overlay LocalOverlay) GTDProjection {
	source = normalizeSourceFields(source)
	overlay = normalizeLocalOverlay(overlay)
	bindings = NormalizeBindings(bindings)
	return GTDProjection{
		Title:        projectionTitle(bindings, source, overlay),
		State:        projectionState(bindings, source, overlay),
		Sphere:       stringValue(overlay.Sphere),
		WorkspaceID:  overlay.WorkspaceID,
		ArtifactID:   overlay.ArtifactID,
		ActorID:      overlay.ActorID,
		SourceKinds:  sourceKinds(bindings),
		Providers:    providers(bindings),
		Labels:       mergeLabels(source.Labels, overlay.Labels),
		DueAt:        source.DueAt,
		VisibleAfter: overlay.VisibleAfter,
		FollowUpAt:   overlay.FollowUpAt,
		ReviewTarget: overlay.ReviewTarget,
		Reviewer:     overlay.Reviewer,
	}
}

func normalizeBinding(binding SourceBinding) SourceBinding {
	binding.Kind = SourceKind(strings.ToLower(strings.TrimSpace(string(binding.Kind))))
	binding.Provider = strings.ToLower(strings.TrimSpace(binding.Provider))
	binding.ObjectType = strings.ToLower(strings.TrimSpace(binding.ObjectType))
	binding.RemoteID = strings.TrimSpace(binding.RemoteID)
	binding.SourceRef = strings.TrimSpace(binding.SourceRef)
	binding.ContainerRef = cleanStringPointer(binding.ContainerRef)
	binding.URL = cleanStringPointer(binding.URL)
	binding.Authority = normalizeAuthority(binding.Kind, binding.Authority)
	return binding
}

func normalizeAuthority(kind SourceKind, authority BackendAuthority) BackendAuthority {
	authority.Backend = strings.ToLower(strings.TrimSpace(authority.Backend))
	if authority.Backend == "" {
		authority.Backend = defaultAuthorityBackend(kind)
	}
	authority.SourceFields = normalizeFieldList(authority.SourceFields)
	authority.LocalOverlayFields = normalizeFieldList(authority.LocalOverlayFields)
	return authority
}

func defaultAuthorityBackend(kind SourceKind) string {
	switch kind {
	case SourceKindMarkdown:
		return string(SourceKindMarkdown)
	case SourceKindTodoist:
		return string(SourceKindTodoist)
	case SourceKindGitHub:
		return string(SourceKindGitHub)
	case SourceKindGitLab:
		return string(SourceKindGitLab)
	case SourceKindEmail:
		return string(SourceKindEmail)
	case SourceKindLocal:
		return string(SourceKindLocal)
	default:
		return ""
	}
}

func normalizeFieldList(fields []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		clean := strings.ToLower(strings.TrimSpace(field))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func normalizeSourceFields(source SourceFields) SourceFields {
	source.Title = strings.TrimSpace(source.Title)
	source.Body = strings.TrimSpace(source.Body)
	source.State = strings.TrimSpace(source.State)
	source.Labels = cleanLabels(source.Labels)
	source.URL = cleanStringPointer(source.URL)
	return source
}

func normalizeLocalOverlay(overlay LocalOverlay) LocalOverlay {
	overlay.Title = cleanStringPointer(overlay.Title)
	overlay.State = cleanStringPointer(overlay.State)
	overlay.Sphere = cleanStringPointer(overlay.Sphere)
	overlay.Labels = cleanLabels(overlay.Labels)
	overlay.ReviewTarget = cleanStringPointer(overlay.ReviewTarget)
	overlay.Reviewer = cleanStringPointer(overlay.Reviewer)
	return overlay
}

func projectionTitle(bindings []SourceBinding, source SourceFields, overlay LocalOverlay) string {
	if overlay.Title != nil && localOwnsField(bindings, "title") {
		return *overlay.Title
	}
	if source.Title != "" {
		return source.Title
	}
	return stringValue(overlay.Title)
}

func projectionState(bindings []SourceBinding, source SourceFields, overlay LocalOverlay) string {
	if overlay.State != nil && localOwnsField(bindings, "state") {
		return normalizeGTDState(*overlay.State)
	}
	if state := normalizeGTDState(source.State); state != "" {
		return state
	}
	if overlay.State != nil {
		return normalizeGTDState(*overlay.State)
	}
	return StateInbox
}

func localOwnsField(bindings []SourceBinding, field string) bool {
	if len(bindings) == 0 {
		return true
	}
	for _, binding := range bindings {
		if contains(binding.Authority.LocalOverlayFields, field) {
			return true
		}
		if contains(binding.Authority.SourceFields, field) {
			return false
		}
	}
	return false
}

func normalizeGTDState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "open", "todo", "active", "needs_action", "unread":
		return StateInbox
	case StateInbox:
		return StateInbox
	case StateWaiting, "delegated", "blocked":
		return StateWaiting
	case StateSomeday, "deferred", "later":
		return StateSomeday
	case StateDone, "closed", "complete", "completed", "read":
		return StateDone
	default:
		return ""
	}
}

func sourceKinds(bindings []SourceBinding) []SourceKind {
	seen := map[SourceKind]struct{}{}
	out := make([]SourceKind, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Kind == "" {
			continue
		}
		if _, ok := seen[binding.Kind]; ok {
			continue
		}
		seen[binding.Kind] = struct{}{}
		out = append(out, binding.Kind)
	}
	return out
}

func providers(bindings []SourceBinding) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Provider == "" {
			continue
		}
		if _, ok := seen[binding.Provider]; ok {
			continue
		}
		seen[binding.Provider] = struct{}{}
		out = append(out, binding.Provider)
	}
	return out
}

func mergeLabels(sourceLabels, overlayLabels []string) []string {
	return cleanLabels(append(append([]string{}, sourceLabels...), overlayLabels...))
}

func cleanLabels(labels []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		clean := strings.ToLower(strings.TrimSpace(label))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func cleanStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
