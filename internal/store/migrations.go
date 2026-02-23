package store

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 2

type migration struct {
	version int
	name    string
	up      string
}

var migrations = []migration{
	{
		version: 1,
		name:    "initial_schema",
		up: `
			CREATE TABLE IF NOT EXISTS jobs (
				id TEXT PRIMARY KEY,
				status TEXT NOT NULL,
				error TEXT,
				pages_scanned INTEGER DEFAULT 0,
				title_page_generated INTEGER DEFAULT 0,
				paperless_doc_id INTEGER,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			);

			CREATE TABLE IF NOT EXISTS state (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
			CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at);
		`,
	},
	{
		version: 2,
		name:    "add_filing_system",
		up: `
			CREATE TABLE IF NOT EXISTS filing_batches (
				filing_id TEXT PRIMARY KEY,
				cumulative_pages INTEGER DEFAULT 0,
				title_page_printed INTEGER DEFAULT 0,
				print_error TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_filing_batches_updated ON filing_batches(updated_at DESC);
			CREATE INDEX IF NOT EXISTS idx_filing_batches_printed ON filing_batches(title_page_printed);

			ALTER TABLE jobs ADD COLUMN filing_id TEXT;
			CREATE INDEX IF NOT EXISTS idx_jobs_filing_id ON jobs(filing_id);
		`,
	},
}

func (s *Store) runMigrations() error {
	currentVersion, err := s.getSchemaVersion()
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	s.logger.Info("database schema", "current_version", currentVersion, "target_version", currentSchemaVersion)

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		s.logger.Info("applying migration", "version", m.version, "name", m.name)

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin transaction for migration %d: %w", m.version, err)
		}

		if _, err := tx.Exec(m.up); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.version, m.name, err)
		}

		if err := s.setSchemaVersionTx(tx, m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("update schema version to %d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}

		s.logger.Info("migration applied successfully", "version", m.version, "name", m.name)
	}

	return nil
}

func (s *Store) getSchemaVersion() (int, error) {
	// First check if state table exists
	var tableExists int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='state'").Scan(&tableExists)
	if err != nil {
		return 0, err
	}

	if tableExists == 0 {
		// No state table means fresh database, version 0
		return 0, nil
	}

	var version int
	err = s.db.QueryRow("SELECT value FROM state WHERE key = 'schema_version'").Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) setSchemaVersionTx(tx *sql.Tx, version int) error {
	_, err := tx.Exec(`
		INSERT INTO state (key, value, updated_at)
		VALUES ('schema_version', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`, version, version)
	return err
}
