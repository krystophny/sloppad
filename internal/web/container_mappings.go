package web

import (
	"net/http"
	"strings"
)

type containerMappingCreateRequest struct {
	Provider      string  `json:"provider"`
	ContainerType string  `json:"container_type"`
	ContainerRef  string  `json:"container_ref"`
	WorkspaceID   *int64  `json:"workspace_id"`
	ProjectID     *string `json:"project_id"`
	Sphere        *string `json:"sphere"`
}

func (a *App) handleContainerMappingList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	mappings, err := a.store.ListContainerMappings(strings.TrimSpace(r.URL.Query().Get("provider")))
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"mappings": mappings,
	})
}

func (a *App) handleContainerMappingCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req containerMappingCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	mapping, err := a.store.SetContainerMapping(
		req.Provider,
		req.ContainerType,
		req.ContainerRef,
		req.WorkspaceID,
		req.ProjectID,
		req.Sphere,
	)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"mapping": mapping,
	})
}

func (a *App) handleContainerMappingDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	mappingID, err := parseURLInt64Param(r, "mapping_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteContainerMapping(mappingID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"deleted":    true,
		"mapping_id": mappingID,
	})
}
