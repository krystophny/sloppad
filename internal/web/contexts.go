package web

import "net/http"

func (a *App) handleLabelList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	labels, err := a.store.ListLabels()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"labels": labels,
	})
}
