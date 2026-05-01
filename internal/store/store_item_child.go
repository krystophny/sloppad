package store

import (
	"database/sql"
	"errors"
	"sort"
)

const itemChildrenTableSchema = `CREATE TABLE IF NOT EXISTS item_children (
  parent_item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  child_item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'next_action' CHECK (role IN ('next_action', 'support', 'blocked_by')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (parent_item_id, child_item_id)
);
CREATE INDEX IF NOT EXISTS idx_item_children_child_item_id
  ON item_children(child_item_id);`

func (s *Store) migrateItemChildLinkSupport() error {
	_, err := s.db.Exec(itemChildrenTableSchema)
	return err
}

func (s *Store) LinkItemChild(parentItemID, childItemID int64, role string) error {
	cleanRole := normalizeItemLinkRole(role)
	if cleanRole == "" {
		return errors.New("item child role must be next_action, support, or blocked_by")
	}
	if parentItemID <= 0 || childItemID <= 0 {
		return errors.New("parent_item_id and child_item_id must be positive integers")
	}
	if parentItemID == childItemID {
		return errors.New("parent_item_id and child_item_id must differ")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	parent, err := scanItem(tx.QueryRow(
		`SELECT id, title, kind, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, due_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`,
		parentItemID,
	))
	if err != nil {
		return err
	}
	if parent.Kind != ItemKindProject {
		return errors.New("parent item must be a project")
	}
	if _, err := scanItem(tx.QueryRow(
		`SELECT id, title, kind, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, due_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`,
		childItemID,
	)); err != nil {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO item_children (parent_item_id, child_item_id, role)
		 VALUES (?, ?, ?)
		 ON CONFLICT(parent_item_id, child_item_id) DO UPDATE SET role = excluded.role`,
		parentItemID,
		childItemID,
		cleanRole,
	); err != nil {
		return err
	}
	if err := s.touchItemTx(tx, parentItemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnlinkItemChild(parentItemID, childItemID int64) error {
	if parentItemID <= 0 || childItemID <= 0 {
		return errors.New("parent_item_id and child_item_id must be positive integers")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRow(
		`SELECT id, title, kind, state, workspace_id, `+scopedContextSelect("context_items", "item_id", "items.id")+` AS sphere, artifact_id, actor_id, visible_after, follow_up_at, due_at, source, source_ref, review_target, reviewer, reviewed_at, created_at, updated_at
		 FROM items
		 WHERE id = ?`,
		parentItemID,
	))
	if err != nil {
		return err
	}
	if item.Kind != ItemKindProject {
		return errors.New("parent item must be a project")
	}
	res, err := tx.Exec(
		`DELETE FROM item_children
		 WHERE parent_item_id = ? AND child_item_id = ?`,
		parentItemID,
		childItemID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if err := s.touchItemTx(tx, parentItemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListItemChildLinks(parentItemID int64) ([]ItemChildLink, error) {
	item, err := s.GetItem(parentItemID)
	if err != nil {
		return nil, err
	}
	if item.Kind != ItemKindProject {
		return nil, errors.New("item is not a project")
	}
	rows, err := s.db.Query(
		`SELECT parent_item_id, child_item_id, role, created_at
		 FROM item_children
		 WHERE parent_item_id = ?
		 ORDER BY CASE role WHEN 'next_action' THEN 0 WHEN 'support' THEN 1 ELSE 2 END,
		          datetime(created_at) ASC,
		          child_item_id ASC`,
		parentItemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ItemChildLink{}
	for rows.Next() {
		var link ItemChildLink
		if err := rows.Scan(&link.ParentItemID, &link.ChildItemID, &link.Role, &link.CreatedAt); err != nil {
			return nil, err
		}
		link.Role = normalizeItemLinkRole(link.Role)
		out = append(out, link)
	}
	return out, rows.Err()
}

// ListProjectItemReviewsFiltered returns the active GTD project-item review
// surface: every Item(kind=project) that is not done, paired with its current
// health and per-state child counts. The list backs the weekly outcome review
// and surfaces stalled outcomes without inventing tasks.
//
// The filter respects sphere/workspace/source/source-container/label/actor
// scoping just like the other GTD list endpoints. Source containers (Todoist
// projects, GitHub Projects) match through the existing `source_container`
// filter — they are never promoted into the review as project items
// themselves. Workspace filtering scopes the project items to a single
// workspace; project items are never converted into workspaces by this query.
//
// Stalled project items sort first; healthy items follow in updated_at desc
// order, so weekly review walks the riskiest outcomes before the rest.
func (s *Store) ListProjectItemReviewsFiltered(filter ItemListFilter) ([]ProjectItemReview, error) {
	normalizedFilter, err := s.prepareItemListFilter(filter)
	if err != nil {
		return nil, err
	}
	parts := []string{"i.kind = ?", "i.state <> ?"}
	args := []any{ItemKindProject, ItemStateDone}
	parts, args = appendItemFilterClauses(parts, args, normalizedFilter, "i.")
	query := itemSummarySelect + ` WHERE ` + stringsJoin(parts, ` AND `) + ` ORDER BY i.updated_at DESC, i.id ASC`
	items, err := s.listItemSummaries(query, args...)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []ProjectItemReview{}, nil
	}
	countsByParent, err := s.collectProjectChildCounts(items)
	if err != nil {
		return nil, err
	}
	reviews := make([]ProjectItemReview, 0, len(items))
	for _, item := range items {
		counts := countsByParent[item.ID]
		reviews = append(reviews, ProjectItemReview{
			Item:     item,
			Children: counts,
			Health:   projectHealthFromCounts(counts),
		})
	}
	sortProjectItemReviewsForWeeklyReview(reviews)
	return reviews, nil
}

// collectProjectChildCounts loads child-state tallies for every project item
// in one round-trip, so the review surface stays O(1) queries regardless of
// how many outcomes are open.
func (s *Store) collectProjectChildCounts(parents []ItemSummary) (map[int64]ProjectChildCounts, error) {
	out := make(map[int64]ProjectChildCounts, len(parents))
	if len(parents) == 0 {
		return out, nil
	}
	placeholders := make([]string, 0, len(parents))
	args := make([]any, 0, len(parents))
	for _, parent := range parents {
		placeholders = append(placeholders, "?")
		args = append(args, parent.ID)
		out[parent.ID] = ProjectChildCounts{}
	}
	rows, err := s.db.Query(
		`SELECT links.parent_item_id, child.state, COUNT(*) AS state_count
		 FROM item_children links
		 JOIN items child ON child.id = links.child_item_id
		 WHERE links.parent_item_id IN (`+stringsJoin(placeholders, ",")+`)
		 GROUP BY links.parent_item_id, child.state`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			parentID int64
			state    string
			count    int
		)
		if err := rows.Scan(&parentID, &state, &count); err != nil {
			return nil, err
		}
		entry := out[parentID]
		entry = applyChildStateCount(entry, state, count)
		out[parentID] = entry
	}
	return out, rows.Err()
}

func applyChildStateCount(counts ProjectChildCounts, state string, count int) ProjectChildCounts {
	if count <= 0 {
		return counts
	}
	switch normalizeItemState(state) {
	case ItemStateInbox:
		counts.Inbox += count
	case ItemStateNext:
		counts.Next += count
	case ItemStateWaiting:
		counts.Waiting += count
	case ItemStateDeferred:
		counts.Deferred += count
	case ItemStateSomeday:
		counts.Someday += count
	case ItemStateReview:
		counts.Review += count
	case ItemStateDone:
		counts.Done += count
	}
	counts.Total += count
	return counts
}

func projectHealthFromCounts(counts ProjectChildCounts) ProjectItemHealth {
	health := ProjectItemHealth{
		HasNextAction: counts.Next > 0,
		HasWaiting:    counts.Waiting > 0,
		HasDeferred:   counts.Deferred > 0,
		HasSomeday:    counts.Someday > 0,
	}
	health.Stalled = !health.HasNextAction && !health.HasWaiting && !health.HasDeferred && !health.HasSomeday
	return health
}

func sortProjectItemReviewsForWeeklyReview(reviews []ProjectItemReview) {
	sort.SliceStable(reviews, func(i, j int) bool {
		if reviews[i].Health.Stalled != reviews[j].Health.Stalled {
			return reviews[i].Health.Stalled
		}
		if reviews[i].Item.UpdatedAt != reviews[j].Item.UpdatedAt {
			return reviews[i].Item.UpdatedAt > reviews[j].Item.UpdatedAt
		}
		return reviews[i].Item.ID < reviews[j].Item.ID
	})
}

func (s *Store) GetProjectItemHealth(itemID int64) (ProjectItemHealth, error) {
	item, err := s.GetItem(itemID)
	if err != nil {
		return ProjectItemHealth{}, err
	}
	if item.Kind != ItemKindProject {
		return ProjectItemHealth{}, errors.New("item is not a project")
	}
	counts, err := s.collectProjectChildCounts([]ItemSummary{{Item: item}})
	if err != nil {
		return ProjectItemHealth{}, err
	}
	return projectHealthFromCounts(counts[itemID]), nil
}

func (s *Store) touchItem(id int64) error {
	res, err := s.db.Exec(`UPDATE items SET updated_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
