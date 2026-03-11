package web

import "net/http"

func (a *App) handleContextList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	contexts, err := a.store.ListContexts()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"contexts": contexts,
	})
}
