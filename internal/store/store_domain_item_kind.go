package store

func (s *Store) migrateItemTableKindSupport() error {
	tableColumns, err := s.tableColumnSet("items")
	if err != nil {
		return err
	}
	if tableColumns["items"]["kind"] {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE items ADD COLUMN kind TEXT NOT NULL DEFAULT 'action' CHECK (kind IN ('action', 'project'))`); err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE items SET kind = 'action' WHERE trim(kind) = ''`)
	return err
}
