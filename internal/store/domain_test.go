package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStoreMigratesDomainTablesOnFreshDatabase(t *testing.T) {
	s := newTestStore(t)

	var foreignKeys int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for table, want := range map[string][]string{
		"workspaces": {"id", "name", "dir_path", "is_active", "created_at", "updated_at"},
		"actors":     {"id", "name", "kind", "created_at"},
		"artifacts":  {"id", "kind", "ref_path", "ref_url", "title", "meta_json", "created_at", "updated_at"},
		"items":      {"id", "title", "state", "workspace_id", "artifact_id", "actor_id", "visible_after", "follow_up_at", "source", "source_ref", "created_at", "updated_at"},
	} {
		got := make(map[string]bool, len(columns[table]))
		for _, name := range columns[table] {
			got[name] = true
		}
		for _, name := range want {
			if !got[name] {
				t.Fatalf("table %s missing column %s: %#v", table, name, columns[table])
			}
		}
	}

	targets := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA foreign_key_list(items)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list(items) error: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, seq                                    int
			table, from, to, onUpdate, onDelete, match string
		)
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		targets[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys: %v", err)
	}
	for _, table := range []string{"workspaces", "artifacts", "actors"} {
		if !targets[table] {
			t.Fatalf("items missing foreign key to %s", table)
		}
	}
}

func TestStoreMigratesDomainTablesOnExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  project_key TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'managed',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE chat_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content_markdown TEXT NOT NULL DEFAULT '',
  content_plain TEXT NOT NULL DEFAULT '',
  render_format TEXT NOT NULL DEFAULT 'markdown',
  created_at INTEGER NOT NULL
);
`
	if _, err := db.Exec(legacySchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() on legacy db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for _, table := range []string{"workspaces", "actors", "artifacts", "items"} {
		if _, ok := columns[table]; !ok {
			t.Fatalf("expected migrated table %s to exist", table)
		}
	}
}

func TestItemSchemaAllowsNilOptionalFields(t *testing.T) {
	s := newTestStore(t)

	res, err := s.db.Exec(`INSERT INTO items (title) VALUES ('triage me')`)
	if err != nil {
		t.Fatalf("insert item without optional fields: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error: %v", err)
	}

	var (
		title                                       string
		workspaceID, artifactID, actorID            sql.NullInt64
		visibleAfter, followUpAt, source, sourceRef sql.NullString
	)
	err = s.db.QueryRow(`
SELECT title, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref
FROM items
WHERE id = ?
`, id).Scan(&title, &workspaceID, &artifactID, &actorID, &visibleAfter, &followUpAt, &source, &sourceRef)
	if err != nil {
		t.Fatalf("query item: %v", err)
	}
	if title != "triage me" {
		t.Fatalf("title = %q, want triage me", title)
	}
	if workspaceID.Valid || artifactID.Valid || actorID.Valid || visibleAfter.Valid || followUpAt.Valid || source.Valid || sourceRef.Valid {
		t.Fatalf("expected optional fields to remain NULL, got workspace=%v artifact=%v actor=%v visible_after=%v follow_up_at=%v source=%v source_ref=%v",
			workspaceID, artifactID, actorID, visibleAfter, followUpAt, source, sourceRef)
	}
}

func TestItemSchemaEnforcesForeignKeys(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(`INSERT INTO items (title, workspace_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing workspace")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, artifact_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing artifact")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, actor_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing actor")
	}
}

func TestDomainTypesExposeJSONTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Workspace", typ: reflect.TypeOf(Workspace{})},
		{name: "Actor", typ: reflect.TypeOf(Actor{})},
		{name: "Artifact", typ: reflect.TypeOf(Artifact{})},
		{name: "Item", typ: reflect.TypeOf(Item{})},
	} {
		for i := 0; i < tc.typ.NumField(); i++ {
			field := tc.typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			if tag := field.Tag.Get("json"); tag == "" || tag == "-" {
				t.Fatalf("%s.%s missing json tag", tc.name, field.Name)
			}
		}
	}
}
