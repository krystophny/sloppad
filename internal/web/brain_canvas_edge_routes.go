package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *App) handleBrainCanvasEdgeCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, ok := a.brainCanvasWorkspace(w, r)
	if !ok {
		return
	}
	var req brainCanvasEdgeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid edge payload", http.StatusBadRequest)
		return
	}
	resp, err := a.addBrainCanvasEdge(workspace, brainCanvasNameFromQuery(r), req)
	a.writeBrainCanvasEdgeMutation(w, resp, err)
}

func (a *App) handleBrainCanvasEdgePromote(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, ok := a.brainCanvasWorkspace(w, r)
	if !ok {
		return
	}
	var req brainCanvasEdgePromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid edge promotion payload", http.StatusBadRequest)
		return
	}
	resp, err := a.promoteBrainCanvasEdge(workspace, brainCanvasNameFromQuery(r), chi.URLParam(r, "edge_id"), req)
	a.writeBrainCanvasEdgeMutation(w, resp, err)
}

func (a *App) handleBrainCanvasEdgeDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, ok := a.brainCanvasWorkspace(w, r)
	if !ok {
		return
	}
	if err := a.deleteBrainCanvasEdge(workspace, brainCanvasNameFromQuery(r), chi.URLParam(r, "edge_id")); err != nil {
		writeBrainCanvasMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) writeBrainCanvasEdgeMutation(w http.ResponseWriter, resp brainCanvasEdgeResponse, err error) {
	if err != nil {
		writeBrainCanvasMutationError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, resp)
}

func writeBrainCanvasMutationError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), brainCanvasMutationStatus(err))
}

func brainCanvasMutationStatus(err error) int {
	switch err.Error() {
	case "card not found", "edge not found", "source card not found", "target card not found":
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}
