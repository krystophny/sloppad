package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/slopshell/internal/store"
)

type brainCanvasEdgeCreateRequest struct {
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	From     string `json:"fromNode"`
	To       string `json:"toNode"`
	Label    string `json:"label,omitempty"`
	Relation string `json:"relation,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Semantic bool   `json:"semantic,omitempty"`
}

type brainCanvasEdgePromoteRequest struct {
	Label    string `json:"label,omitempty"`
	Relation string `json:"relation,omitempty"`
}

type brainCanvasEdge struct {
	ID                 string `json:"id"`
	From               string `json:"fromNode"`
	To                 string `json:"toNode"`
	Label              string `json:"label,omitempty"`
	Relation           string `json:"relation,omitempty"`
	Mode               string `json:"mode,omitempty"`
	ProposalItemID     int64  `json:"proposal_item_id,omitempty"`
	ProposalArtifactID int64  `json:"proposal_artifact_id,omitempty"`
}

type brainCanvasEdgeResponse struct {
	OK                   bool            `json:"ok"`
	Edge                 brainCanvasEdge `json:"edge"`
	ProposalItemID       int64           `json:"proposal_item_id,omitempty"`
	ProposalArtifactID   int64           `json:"proposal_artifact_id,omitempty"`
	ProposalArtifactPath string          `json:"proposal_artifact_path,omitempty"`
	ProposalItem         *store.Item     `json:"proposal_item,omitempty"`
	ProposalArtifact     *store.Artifact `json:"proposal_artifact,omitempty"`
}

type brainCanvasEdgeProposal struct {
	item         store.Item
	artifact     store.Artifact
	artifactPath string
}

const (
	brainCanvasEdgeModeVisual     = "visual"
	brainCanvasEdgeModeSemantic   = "semantic"
	brainCanvasProposalSource     = "brain_canvas_relation"
	brainCanvasProposalDir        = ".slopshell/artifacts/brain-canvas-relations"
	brainCanvasMaxEdgeLabelLength = 160
	brainCanvasMaxRelationLength  = 64
	brainCanvasEdgeIDByteSize     = 8
)

func newBrainCanvasEdgeID() string {
	return newBrainCanvasID("edge", brainCanvasEdgeIDByteSize)
}

func (req brainCanvasEdgeCreateRequest) endpoints() (string, string) {
	from := strings.TrimSpace(req.FromNode)
	if from == "" {
		from = strings.TrimSpace(req.From)
	}
	to := strings.TrimSpace(req.ToNode)
	if to == "" {
		to = strings.TrimSpace(req.To)
	}
	return from, to
}

func (req brainCanvasEdgeCreateRequest) edgeMode() string {
	if req.Semantic {
		return brainCanvasEdgeModeSemantic
	}
	return normalizeBrainCanvasEdgeMode(req.Mode)
}

func normalizeBrainCanvasEdgeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", brainCanvasEdgeModeVisual:
		return brainCanvasEdgeModeVisual
	case brainCanvasEdgeModeSemantic:
		return brainCanvasEdgeModeSemantic
	default:
		return ""
	}
}

func normalizeBrainCanvasEdgeLabel(raw string) (string, error) {
	label := strings.Join(strings.Fields(raw), " ")
	if len(label) > brainCanvasMaxEdgeLabelLength {
		return "", fmt.Errorf("edge label must be at most %d characters", brainCanvasMaxEdgeLabelLength)
	}
	return label, nil
}

func normalizeBrainCanvasRelation(raw string) (string, error) {
	relation := strings.ToLower(strings.TrimSpace(raw))
	if relation == "" {
		return "", nil
	}
	if len(relation) > brainCanvasMaxRelationLength {
		return "", fmt.Errorf("edge relation must be at most %d characters", brainCanvasMaxRelationLength)
	}
	for _, r := range relation {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return "", errors.New("edge relation must be a slug")
	}
	return relation, nil
}

func (a *App) addBrainCanvasEdge(workspace store.Workspace, name string, req brainCanvasEdgeCreateRequest) (brainCanvasEdgeResponse, error) {
	resolver, canvasPath, doc, err := a.loadBrainCanvasEdgeDocument(workspace, name)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	edge, err := normalizeBrainCanvasEdge(req)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	source, target, err := validateBrainCanvasEdge(resolver, doc, edge)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	if hasBrainCanvasEdge(doc.Edges, edge) {
		return brainCanvasEdgeResponse{}, errors.New("canvas edge already exists")
	}
	proposal, err := a.createBrainCanvasProposal(resolver, name, &edge, source, target)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	doc.Edges = append(doc.Edges, edge)
	if err := writeBrainCanvasDocument(canvasPath, doc); err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	return brainCanvasEdgeReply(edge, proposal), nil
}

func normalizeBrainCanvasEdge(req brainCanvasEdgeCreateRequest) (brainCanvasEdge, error) {
	from, to := req.endpoints()
	label, err := normalizeBrainCanvasEdgeLabel(req.Label)
	if err != nil {
		return brainCanvasEdge{}, err
	}
	relation, err := normalizeBrainCanvasRelation(req.Relation)
	if err != nil {
		return brainCanvasEdge{}, err
	}
	mode := req.edgeMode()
	if mode == "" {
		return brainCanvasEdge{}, errors.New("edge mode must be visual or semantic")
	}
	return brainCanvasEdge{ID: newBrainCanvasEdgeID(), From: from, To: to, Label: label, Relation: relation, Mode: mode}, nil
}

func (a *App) promoteBrainCanvasEdge(workspace store.Workspace, name, edgeID string, req brainCanvasEdgePromoteRequest) (brainCanvasEdgeResponse, error) {
	resolver, canvasPath, doc, err := a.loadBrainCanvasEdgeDocument(workspace, name)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	idx := indexOfBrainCanvasEdge(doc.Edges, edgeID)
	if idx < 0 {
		return brainCanvasEdgeResponse{}, errors.New("edge not found")
	}
	edge := doc.Edges[idx]
	if edge.Mode == brainCanvasEdgeModeSemantic {
		return brainCanvasEdgeResponse{}, errors.New("edge already has a semantic proposal")
	}
	if err := applyBrainCanvasEdgePromotion(&edge, req); err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	source, target, err := validateBrainCanvasEdge(resolver, doc, edge)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	proposal, err := a.createBrainCanvasProposal(resolver, name, &edge, source, target)
	if err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	doc.Edges[idx] = edge
	if err := writeBrainCanvasDocument(canvasPath, doc); err != nil {
		return brainCanvasEdgeResponse{}, err
	}
	return brainCanvasEdgeReply(edge, proposal), nil
}

func applyBrainCanvasEdgePromotion(edge *brainCanvasEdge, req brainCanvasEdgePromoteRequest) error {
	if strings.TrimSpace(req.Label) != "" {
		label, err := normalizeBrainCanvasEdgeLabel(req.Label)
		if err != nil {
			return err
		}
		edge.Label = label
	}
	relation, err := normalizeBrainCanvasRelation(req.Relation)
	if err != nil {
		return err
	}
	if relation != "" {
		edge.Relation = relation
	}
	edge.Mode = brainCanvasEdgeModeSemantic
	return nil
}

func (a *App) deleteBrainCanvasEdge(workspace store.Workspace, name, edgeID string) error {
	_, canvasPath, doc, err := a.loadBrainCanvasEdgeDocument(workspace, name)
	if err != nil {
		return err
	}
	idx := indexOfBrainCanvasEdge(doc.Edges, edgeID)
	if idx < 0 {
		return errors.New("edge not found")
	}
	doc.Edges = append(doc.Edges[:idx], doc.Edges[idx+1:]...)
	return writeBrainCanvasDocument(canvasPath, doc)
}

func (a *App) loadBrainCanvasEdgeDocument(workspace store.Workspace, name string) (brainCanvasResolver, string, brainCanvasDocument, error) {
	resolver, err := newBrainCanvasResolver(a.store, workspace)
	if err != nil {
		return brainCanvasResolver{}, "", brainCanvasDocument{}, err
	}
	canvasPath, err := brainCanvasFilePath(resolver.brainRoot, name)
	if err != nil {
		return brainCanvasResolver{}, "", brainCanvasDocument{}, err
	}
	doc, err := loadBrainCanvasDocument(canvasPath)
	return resolver, canvasPath, doc, err
}

func validateBrainCanvasEdge(resolver brainCanvasResolver, doc brainCanvasDocument, edge brainCanvasEdge) (brainCanvasNode, brainCanvasNode, error) {
	if edge.From == "" || edge.To == "" {
		return brainCanvasNode{}, brainCanvasNode{}, errors.New("edge endpoints are required")
	}
	if edge.From == edge.To {
		return brainCanvasNode{}, brainCanvasNode{}, errors.New("edge endpoints must be different cards")
	}
	if edge.Mode == brainCanvasEdgeModeSemantic && edge.Relation == "" {
		return brainCanvasNode{}, brainCanvasNode{}, errors.New("semantic edge requires relation")
	}
	source, ok := brainCanvasNodeByID(doc.Nodes, edge.From)
	if !ok {
		return brainCanvasNode{}, brainCanvasNode{}, errors.New("source card not found")
	}
	target, ok := brainCanvasNodeByID(doc.Nodes, edge.To)
	if !ok {
		return brainCanvasNode{}, brainCanvasNode{}, errors.New("target card not found")
	}
	return source, target, validateBrainCanvasEdgeBindings(resolver, source, target)
}

func validateBrainCanvasEdgeBindings(resolver brainCanvasResolver, source, target brainCanvasNode) error {
	if source.Binding.Kind == "" || target.Binding.Kind == "" {
		return errors.New("edge endpoints must be backed cards")
	}
	if err := assertBrainCanvasBindingExists(resolver, source.Binding); err != nil {
		return fmt.Errorf("source card binding invalid: %w", err)
	}
	if err := assertBrainCanvasBindingExists(resolver, target.Binding); err != nil {
		return fmt.Errorf("target card binding invalid: %w", err)
	}
	return nil
}

func brainCanvasNodeByID(nodes []brainCanvasNode, id string) (brainCanvasNode, bool) {
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return brainCanvasNode{}, false
}

func indexOfBrainCanvasEdge(edges []brainCanvasEdge, edgeID string) int {
	for i, edge := range edges {
		if edge.ID == edgeID {
			return i
		}
	}
	return -1
}

func hasBrainCanvasEdge(edges []brainCanvasEdge, next brainCanvasEdge) bool {
	for _, edge := range edges {
		if edge.From == next.From && edge.To == next.To && edge.Relation == next.Relation && edge.Mode == next.Mode {
			return true
		}
	}
	return false
}

func (a *App) createBrainCanvasProposal(resolver brainCanvasResolver, name string, edge *brainCanvasEdge, source, target brainCanvasNode) (*brainCanvasEdgeProposal, error) {
	if edge.Mode != brainCanvasEdgeModeSemantic {
		return nil, nil
	}
	body := brainCanvasProposalMarkdown(resolver, name, *edge, source, target)
	relPath, err := writeBrainCanvasProposalFile(resolver.workspace.RootPath, edge.ID, body)
	if err != nil {
		return nil, err
	}
	title := fmt.Sprintf("Review canvas relation: %s", brainCanvasRelationTitle(*edge, source, target))
	artifact, err := a.store.CreateArtifact(store.ArtifactKindMarkdown, &relPath, nil, &title, nil)
	if err != nil {
		return nil, err
	}
	sourceRef := edge.ID
	item, err := a.store.CreateItem(title, store.ItemOptions{
		State:       store.ItemStateReview,
		Kind:        store.ItemKindAction,
		WorkspaceID: &resolver.workspace.ID,
		Sphere:      &resolver.workspace.Sphere,
		ArtifactID:  &artifact.ID,
		Source:      brainCanvasStringPointer(brainCanvasProposalSource),
		SourceRef:   &sourceRef,
	})
	if err != nil {
		return nil, err
	}
	edge.ProposalItemID = item.ID
	edge.ProposalArtifactID = artifact.ID
	return &brainCanvasEdgeProposal{item: item, artifact: artifact, artifactPath: relPath}, nil
}

func writeBrainCanvasProposalFile(root, edgeID, body string) (string, error) {
	dir := filepath.Join(root, brainCanvasProposalDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	relPath := filepath.ToSlash(filepath.Join(brainCanvasProposalDir, edgeID+".md"))
	return relPath, os.WriteFile(filepath.Join(root, filepath.FromSlash(relPath)), []byte(body), 0o644)
}

func brainCanvasProposalMarkdown(resolver brainCanvasResolver, canvasName string, edge brainCanvasEdge, source, target brainCanvasNode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Canvas relation proposal\n\n")
	fmt.Fprintf(&b, "- Canvas: `%s`\n", normalizeBrainCanvasName(canvasName))
	fmt.Fprintf(&b, "- Edge: `%s`\n", edge.ID)
	fmt.Fprintf(&b, "- Relation: `%s`\n", edge.Relation)
	if edge.Label != "" {
		fmt.Fprintf(&b, "- Label: `%s`\n", edge.Label)
	}
	fmt.Fprintf(&b, "- Source: `%s`\n", brainCanvasCardDescriptor(resolver, source))
	fmt.Fprintf(&b, "- Target: `%s`\n", brainCanvasCardDescriptor(resolver, target))
	b.WriteString("\n## Proposed Markdown update\n\n")
	b.WriteString(brainCanvasProposedMarkdownUpdate(source, target, edge.Relation))
	b.WriteString("\n\nReview this proposal before editing canonical notes.\n")
	return b.String()
}

func brainCanvasProposedMarkdownUpdate(source, target brainCanvasNode, relation string) string {
	if source.Binding.Kind == "note" && target.Binding.Kind == "note" {
		return fmt.Sprintf("Add a `%s` relation from `%s` to `[[%s]]`.", relation, source.Binding.Path, target.Binding.Path)
	}
	return fmt.Sprintf("Add a `%s` relation from `%s` to `%s`.", relation, brainCanvasBindingRef(source.Binding), brainCanvasBindingRef(target.Binding))
}

func brainCanvasCardDescriptor(resolver brainCanvasResolver, node brainCanvasNode) string {
	view := resolver.resolveCardView(node)
	return strings.TrimSpace(view.Title + " (" + brainCanvasBindingRef(node.Binding) + ")")
}

func brainCanvasBindingRef(binding brainCanvasBinding) string {
	switch binding.Kind {
	case "artifact", "item", "actor", "label":
		return fmt.Sprintf("%s:%d", binding.Kind, binding.ID)
	case "note":
		return "note:" + binding.Path
	case "link":
		return "link:" + binding.URL
	case "source":
		return "source:" + binding.Provider + ":" + binding.Ref
	default:
		return binding.Kind
	}
}

func brainCanvasRelationTitle(edge brainCanvasEdge, source, target brainCanvasNode) string {
	label := edge.Relation
	if label == "" {
		label = edge.Label
	}
	return fmt.Sprintf("%s -> %s (%s)", source.ID, target.ID, label)
}

func brainCanvasEdgeReply(edge brainCanvasEdge, proposal *brainCanvasEdgeProposal) brainCanvasEdgeResponse {
	resp := brainCanvasEdgeResponse{OK: true, Edge: edge}
	if proposal == nil {
		return resp
	}
	resp.ProposalItemID = proposal.item.ID
	resp.ProposalArtifactID = proposal.artifact.ID
	resp.ProposalArtifactPath = proposal.artifactPath
	resp.ProposalItem = &proposal.item
	resp.ProposalArtifact = &proposal.artifact
	return resp
}

func brainCanvasStringPointer(value string) *string {
	return &value
}

func pruneBrainCanvasEdgesForNode(edges []brainCanvasEdge, nodeID string) []brainCanvasEdge {
	out := edges[:0]
	for _, edge := range edges {
		if edge.From == nodeID || edge.To == nodeID {
			continue
		}
		out = append(out, edge)
	}
	return out
}
