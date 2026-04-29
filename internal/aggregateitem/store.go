package aggregateitem

import (
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

func FromStoreItem(item store.Item, bindings []store.ExternalBinding) (AggregateItem, error) {
	aggregateBindings := bindingsFromStore(item, bindings)
	source := SourceFields{
		Title: item.Title,
		State: item.State,
	}
	overlay, err := overlayFromStoreItem(item)
	if err != nil {
		return AggregateItem{}, err
	}
	return New(storeItemAggregateID(item), aggregateBindings, source, overlay)
}

func bindingsFromStore(item store.Item, bindings []store.ExternalBinding) []SourceBinding {
	out := make([]SourceBinding, 0, len(bindings)+1)
	for _, binding := range bindings {
		out = append(out, bindingFromExternalBinding(binding))
	}
	if len(out) == 0 {
		out = append(out, bindingFromItemSource(item))
	}
	return out
}

func bindingFromExternalBinding(binding store.ExternalBinding) SourceBinding {
	return SourceBinding{
		Kind:            sourceKindFromProvider(binding.Provider),
		Provider:        binding.Provider,
		AccountID:       positiveIDPointer(binding.AccountID),
		ObjectType:      binding.ObjectType,
		RemoteID:        binding.RemoteID,
		SourceRef:       binding.RemoteID,
		ContainerRef:    binding.ContainerRef,
		RemoteUpdatedAt: parseOptionalStoreTime(binding.RemoteUpdatedAt),
		Authority:       storeBindingAuthority(sourceKindFromProvider(binding.Provider)),
	}
}

func bindingFromItemSource(item store.Item) SourceBinding {
	source := strings.TrimSpace(stringValue(item.Source))
	sourceRef := strings.TrimSpace(stringValue(item.SourceRef))
	kind := sourceKindFromProvider(source)
	if kind == SourceKindMarkdown || kind == SourceKindLocal {
		if sourceRef == "" && item.ID > 0 {
			sourceRef = fmt.Sprintf("item:%d", item.ID)
		}
		return SourceBinding{
			Kind:      kind,
			SourceRef: sourceRef,
			Authority: storeBindingAuthority(kind),
		}
	}
	return SourceBinding{
		Kind:       kind,
		Provider:   source,
		ObjectType: defaultObjectType(kind),
		RemoteID:   sourceRef,
		SourceRef:  sourceRef,
		Authority:  storeBindingAuthority(kind),
	}
}

func overlayFromStoreItem(item store.Item) (LocalOverlay, error) {
	visibleAfter, err := parseStoreTime(item.VisibleAfter)
	if err != nil {
		return LocalOverlay{}, err
	}
	followUpAt, err := parseStoreTime(item.FollowUpAt)
	if err != nil {
		return LocalOverlay{}, err
	}
	sphere := item.Sphere
	return LocalOverlay{
		WorkspaceID:  item.WorkspaceID,
		Sphere:       &sphere,
		ArtifactID:   item.ArtifactID,
		ActorID:      item.ActorID,
		VisibleAfter: visibleAfter,
		FollowUpAt:   followUpAt,
		ReviewTarget: item.ReviewTarget,
		Reviewer:     item.Reviewer,
	}, nil
}

func storeItemAggregateID(item store.Item) string {
	if item.Source != nil && item.SourceRef != nil {
		source := strings.TrimSpace(*item.Source)
		sourceRef := strings.TrimSpace(*item.SourceRef)
		if source != "" && sourceRef != "" {
			return source + ":" + sourceRef
		}
	}
	if item.ID > 0 {
		return fmt.Sprintf("item:%d", item.ID)
	}
	return ""
}

func sourceKindFromProvider(provider string) SourceKind {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case string(SourceKindMarkdown):
		return SourceKindMarkdown
	case store.ExternalProviderTodoist:
		return SourceKindTodoist
	case string(SourceKindGitHub), "bug_report":
		return SourceKindGitHub
	case string(SourceKindGitLab):
		return SourceKindGitLab
	case store.ExternalProviderGmail, store.ExternalProviderIMAP, store.ExternalProviderExchange, store.ExternalProviderExchangeEWS, "email":
		return SourceKindEmail
	case string(SourceKindLocal), "":
		return SourceKindLocal
	default:
		return SourceKindLocal
	}
}

func defaultObjectType(kind SourceKind) string {
	switch kind {
	case SourceKindTodoist:
		return "task"
	case SourceKindGitHub, SourceKindGitLab:
		return "issue"
	case SourceKindEmail:
		return "email"
	default:
		return ""
	}
}

func storeBindingAuthority(kind SourceKind) BackendAuthority {
	localFields := []string{"workspace_id", "sphere", "artifact_id", "actor_id", "visible_after", "follow_up_at", "review_target", "reviewer"}
	if kind == SourceKindLocal {
		localFields = append(localFields, "title", "state")
		return BackendAuthority{
			Backend:            string(SourceKindLocal),
			LocalOverlayFields: localFields,
		}
	}
	return BackendAuthority{
		Backend:            defaultAuthorityBackend(kind),
		SourceFields:       []string{"title", "state", "due_at", "labels"},
		LocalOverlayFields: localFields,
	}
}

func positiveIDPointer(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func parseOptionalStoreTime(value *string) *time.Time {
	parsed, _ := parseStoreTime(value)
	return parsed
}

func parseStoreTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*value))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
