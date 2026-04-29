package web

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sloppy-org/slopshell/internal/store"
)

type workspaceMarkdownLinkResolution struct {
	OK                bool   `json:"ok"`
	Blocked           bool   `json:"blocked,omitempty"`
	Reason            string `json:"reason,omitempty"`
	SourcePath        string `json:"source_path,omitempty"`
	Target            string `json:"target,omitempty"`
	ResolvedPath      string `json:"resolved_path,omitempty"`
	VaultRelativePath string `json:"vault_relative_path,omitempty"`
	FileURL           string `json:"file_url,omitempty"`
	Kind              string `json:"kind,omitempty"`
}

func brainWorkspaceRoots(workspace store.Workspace) (string, string, error) {
	root := absoluteCleanPath(workspace.DirPath)
	if root == "" {
		return "", "", errors.New("workspace path is required")
	}
	if filepath.Base(root) == "brain" {
		return root, filepath.Dir(root), nil
	}
	return root, root, nil
}

func normalizeMarkdownSourcePath(raw string) (string, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return "", errors.New("source path is required")
	}
	return normalizeProjectListPath(clean)
}

func cleanMarkdownLinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">"))
	}
	if strings.HasPrefix(strings.ToLower(target), "slopshell-wiki:") {
		decoded, err := url.PathUnescape(target[len("slopshell-wiki:"):])
		if err == nil {
			target = decoded
		}
	}
	if idx := strings.Index(target, "|"); idx >= 0 {
		target = strings.TrimSpace(target[:idx])
	}
	if idx := strings.Index(target, "#"); idx >= 0 {
		target = strings.TrimSpace(target[:idx])
	}
	if idx := strings.Index(target, "?"); idx >= 0 {
		target = strings.TrimSpace(target[:idx])
	}
	return strings.TrimSpace(target)
}

func isExternalMarkdownLink(raw string) bool {
	target := strings.TrimSpace(raw)
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "#") {
		return true
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	return !strings.EqualFold(parsed.Scheme, "file") && !strings.EqualFold(parsed.Scheme, "slopshell-wiki")
}

func markdownLinkKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".pdf":
		return "pdf"
	default:
		return "text"
	}
}

func markdownLinkCandidatePaths(sourceAbs, brainRoot, vaultRoot, target string, wikilink bool) []string {
	if target == "" {
		return []string{sourceAbs}
	}
	candidates := []string{}
	add := func(path string) {
		clean := filepath.Clean(path)
		for _, existing := range candidates {
			if existing == clean {
				return
			}
		}
		candidates = append(candidates, clean)
	}
	addWithMarkdown := func(path string) {
		add(path)
		if filepath.Ext(path) == "" {
			add(path + ".md")
		}
	}
	if filepath.IsAbs(target) {
		addWithMarkdown(target)
		return candidates
	}
	normalized := strings.ReplaceAll(target, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		addWithMarkdown(filepath.Join(vaultRoot, strings.TrimPrefix(normalized, "/")))
		return candidates
	}
	addWithMarkdown(filepath.Join(filepath.Dir(sourceAbs), filepath.FromSlash(normalized)))
	if wikilink {
		addWithMarkdown(filepath.Join(brainRoot, filepath.FromSlash(normalized)))
		addWithMarkdown(filepath.Join(vaultRoot, filepath.FromSlash(normalized)))
	}
	return candidates
}

func findWikilinkByBasename(brainRoot, target string) (string, error) {
	if target == "" || strings.ContainsAny(target, `/\`) {
		return "", os.ErrNotExist
	}
	name := target
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	matches := []string{}
	err := filepath.WalkDir(brainRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if pathInWorkPersonalGuardrail(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(entry.Name(), name) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", os.ErrNotExist
}

func resolveWorkspaceMarkdownLink(workspace store.Workspace, sourceRaw, targetRaw, linkType string) workspaceMarkdownLinkResolution {
	result := workspaceMarkdownLinkResolution{
		Target: strings.TrimSpace(targetRaw),
	}
	if isExternalMarkdownLink(targetRaw) {
		result.Blocked = true
		result.Reason = "external links open outside the workspace"
		return result
	}
	brainRoot, vaultRoot, err := brainWorkspaceRoots(workspace)
	if err != nil {
		result.Blocked = true
		result.Reason = err.Error()
		return result
	}
	sourceRel, err := normalizeMarkdownSourcePath(sourceRaw)
	if err != nil {
		result.Blocked = true
		result.Reason = err.Error()
		return result
	}
	sourceAbs := filepath.Clean(filepath.Join(brainRoot, filepath.FromSlash(sourceRel)))
	if !pathInsideOrEqual(sourceAbs, brainRoot) {
		result.Blocked = true
		result.Reason = "source note is outside the brain workspace"
		return result
	}
	result.SourcePath = sourceRel
	target := cleanMarkdownLinkTarget(targetRaw)
	wikilink := strings.EqualFold(strings.TrimSpace(linkType), "wikilink") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(targetRaw)), "slopshell-wiki:")
	candidates := markdownLinkCandidatePaths(sourceAbs, brainRoot, vaultRoot, target, wikilink)
	if wikilink {
		if found, err := findWikilinkByBasename(brainRoot, target); err == nil {
			candidates = append(candidates, found)
		}
	}
	for _, candidate := range candidates {
		if !pathInsideOrEqual(candidate, vaultRoot) {
			result.Blocked = true
			result.Reason = "link target leaves the vault"
			return result
		}
		if err := enforceWorkPersonalPath(candidate); err != nil {
			result.Blocked = true
			result.Reason = workPersonalGuardrailMessage
			return result
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		vaultRel, err := filepath.Rel(vaultRoot, candidate)
		if err != nil {
			result.Blocked = true
			result.Reason = "link target leaves the vault"
			return result
		}
		vaultRel = filepath.ToSlash(filepath.Clean(vaultRel))
		result.OK = true
		result.ResolvedPath = vaultRel
		result.VaultRelativePath = vaultRel
		result.Kind = markdownLinkKind(candidate)
		result.FileURL = "/api/workspaces/" + workspaceIDStr(workspace.ID) + "/markdown-link/file?path=" + url.QueryEscape(vaultRel)
		return result
	}
	result.Blocked = true
	result.Reason = "link target was not found in the vault"
	return result
}

func (a *App) handleWorkspaceMarkdownLinkResolve(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, err := a.resolveRuntimeWorkspaceByIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := enforceWorkPersonalWorkspace(workspace); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	result := resolveWorkspaceMarkdownLink(workspace, r.URL.Query().Get("source"), r.URL.Query().Get("target"), r.URL.Query().Get("type"))
	writeJSON(w, result)
}

func (a *App) handleWorkspaceMarkdownLinkFile(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, err := a.resolveRuntimeWorkspaceByIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := enforceWorkPersonalWorkspace(workspace); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	_, vaultRoot, err := brainWorkspaceRoots(workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	relPath, err := normalizeProjectListPath(r.URL.Query().Get("path"))
	if err != nil || relPath == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	targetPath := filepath.Clean(filepath.Join(vaultRoot, filepath.FromSlash(relPath)))
	if !pathInsideOrEqual(targetPath, vaultRoot) {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}
	if err := enforceWorkPersonalPath(targetPath); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, targetPath)
}
