package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/googleauth"
)

func (a *App) handleGoogleAuthStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	redirectURI := googleAuthCallbackURI(r)
	authURL := session.GetAuthURLWithRedirect(redirectURI)
	if authURL == "" {
		writeAPIError(w, http.StatusInternalServerError, "could not generate auth URL")
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (a *App) handleGoogleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectURI := googleAuthCallbackURI(r)
	if err := session.ExchangeCodeWithRedirect(context.Background(), code, redirectURI); err != nil {
		http.Error(w, fmt.Sprintf("Google auth failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<!DOCTYPE html><html><body><h1>Google connected</h1><p>You can close this tab.</p><script>setTimeout(function(){window.close()},2000)</script></body></html>`)
}

func googleAuthCallbackURI(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host + "/api/google/callback"
}
