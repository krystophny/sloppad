package web

import (
	"net/http"

	"github.com/sloppy-org/slopshell/internal/store"
)

// handleItemProjectReview answers the GTD composite-outcome review surface.
//
// The response lists every active Item(kind=project) — workspace records and
// external source containers (Todoist projects, GitHub Projects, mail folders)
// are intentionally absent. Each row carries the project item's current health
// flags and per-state child counts so the weekly review can spot stalled
// outcomes without inventing tasks.
func (a *App) handleItemProjectReview(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if !a.resurfaceDueItemsForRead(w) {
		return
	}
	filter, err := parseItemListFilterQuery(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	reviews, err := a.store.ListProjectItemReviewsFiltered(filter)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	stalled := countStalledProjectItems(reviews)
	writeAPIData(w, http.StatusOK, map[string]any{
		"project_items": reviews,
		"total":         len(reviews),
		"stalled":       stalled,
	})
}

func countStalledProjectItems(reviews []store.ProjectItemReview) int {
	stalled := 0
	for _, review := range reviews {
		if review.Health.Stalled {
			stalled++
		}
	}
	return stalled
}
