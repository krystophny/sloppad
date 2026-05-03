package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sloppy-org/slopshell/internal/projection"
)

func (s *Store) migrateProjectionBoundary() error {
	return s.scrubRemoteProjectionArtifactBodies()
}

func (s *Store) scrubRemoteProjectionArtifactBodies() error {
	rows, err := s.db.Query(
		`SELECT id, kind, meta_json
		 FROM artifacts
		 WHERE kind IN (?, ?, ?)`,
		ArtifactKindEmail,
		ArtifactKindEmailThread,
		ArtifactKindExternalTask,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type artifactRow struct {
		id       int64
		kind     ArtifactKind
		metaJSON string
	}

	var artifacts []artifactRow
	for rows.Next() {
		var row artifactRow
		if err := rows.Scan(&row.id, &row.kind, &row.metaJSON); err != nil {
			return err
		}
		artifacts = append(artifacts, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, artifact := range artifacts {
		cleaned, changed, err := s.scrubProjectionArtifactMeta(artifact.id, artifact.kind, artifact.metaJSON)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if _, err := s.db.Exec(
			`UPDATE artifacts
			 SET meta_json = ?, updated_at = datetime('now')
			 WHERE id = ?`,
			cleaned,
			artifact.id,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) scrubProjectionArtifactMeta(id int64, kind ArtifactKind, raw string) (string, bool, error) {
	cleanRaw := strings.TrimSpace(raw)
	if cleanRaw == "" {
		return raw, false, nil
	}

	var meta map[string]any
	if err := json.Unmarshal([]byte(cleanRaw), &meta); err != nil {
		return "", false, fmt.Errorf("decode artifact %d meta_json: %w", id, err)
	}

	var (
		changed bool
		err     error
	)
	switch normalizeArtifactKind(kind) {
	case ArtifactKindEmail:
		var remote bool
		remote, err = s.artifactHasExternalBinding(id, "email")
		if remote {
			changed = scrubProjectionEmailMeta(meta)
		}
	case ArtifactKindEmailThread:
		var remote bool
		remote, err = s.artifactHasExternalBinding(id, "email_thread")
		if remote {
			changed = scrubProjectionEmailThreadMeta(meta)
		}
	case ArtifactKindExternalTask:
		var remote bool
		remote, err = s.artifactIsExchangeTaskProjection(id)
		if remote {
			changed = scrubProjectionExchangeTaskMeta(meta)
		}
	}
	if err != nil {
		return "", false, err
	}
	if !changed {
		return raw, false, nil
	}

	encoded, err := json.Marshal(meta)
	if err != nil {
		return "", false, fmt.Errorf("marshal artifact %d meta_json: %w", id, err)
	}
	return string(encoded), true, nil
}

func (s *Store) artifactHasExternalBinding(artifactID int64, objectType string) (bool, error) {
	var marker int
	err := s.db.QueryRow(
		`SELECT 1
		 FROM external_bindings
		 WHERE artifact_id = ? AND object_type = ?
		 LIMIT 1`,
		artifactID,
		strings.TrimSpace(objectType),
	).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

func (s *Store) artifactIsExchangeTaskProjection(artifactID int64) (bool, error) {
	var marker int
	err := s.db.QueryRow(
		`SELECT 1
		 FROM items
		 WHERE artifact_id = ? AND source = ? AND source_ref LIKE 'task:%'
		 LIMIT 1`,
		artifactID,
		ExternalProviderExchangeEWS,
	).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

func scrubProjectionEmailMeta(meta map[string]any) bool {
	return scrubProjectionBodyField(meta, "snippet")
}

func scrubProjectionExchangeTaskMeta(meta map[string]any) bool {
	return scrubProjectionBodyField(meta, "summary")
}

func scrubProjectionBodyField(meta map[string]any, previewKey string) bool {
	body := projectionString(meta["body"])
	changed := false
	if previewKey != "" && strings.TrimSpace(projectionString(meta[previewKey])) == "" {
		if preview := projection.PreviewText(body); preview != "" {
			meta[previewKey] = preview
			changed = true
		}
	}
	if _, ok := meta["body"]; ok {
		delete(meta, "body")
		changed = true
	}
	return changed
}

func scrubProjectionEmailThreadMeta(meta map[string]any) bool {
	changed := scrubProjectionBodyField(meta, "snippet")
	records, ok := meta["messages"].([]any)
	if !ok {
		return changed
	}
	for i := range records {
		record, ok := records[i].(map[string]any)
		if !ok {
			continue
		}
		if scrubProjectionBodyField(record, "snippet") {
			records[i] = record
			changed = true
		}
	}
	if changed {
		meta["messages"] = records
	}
	return changed
}

func projectionString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
