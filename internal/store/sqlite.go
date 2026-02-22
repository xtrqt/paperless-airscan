package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

var schema = `
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
`

func New(databasePath string) (*Store, error) {
	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateJob(id string) error {
	_, err := s.db.Exec(
		"INSERT INTO jobs (id, status, created_at, updated_at) VALUES (?, ?, ?, ?)",
		id, StatusPending, time.Now(), time.Now(),
	)
	return err
}

func (s *Store) GetJob(id string) (*Job, error) {
	job := &Job{}
	var titlePageGenerated int
	var paperlessDocID sql.NullInt64
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		"SELECT id, status, error, pages_scanned, title_page_generated, paperless_doc_id, created_at, updated_at FROM jobs WHERE id = ?",
		id,
	).Scan(&job.ID, &job.Status, &job.Error, &job.PagesScanned, &titlePageGenerated, &paperlessDocID, &createdAt, &updatedAt)

	if err != nil {
		return nil, err
	}

	job.TitlePageGenerated = titlePageGenerated == 1
	if paperlessDocID.Valid {
		job.PaperlessDocID = int(paperlessDocID.Int64)
	}

	job.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	job.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return job, nil
}

func (s *Store) UpdateJobStatus(id string, status JobStatus, errMsg string) error {
	_, err := s.db.Exec(
		"UPDATE jobs SET status = ?, error = ?, updated_at = ? WHERE id = ?",
		status, errMsg, time.Now(), id,
	)
	return err
}

func (s *Store) UpdateJobProgress(id string, pagesScanned int, titlePageGenerated bool) error {
	_, err := s.db.Exec(
		"UPDATE jobs SET pages_scanned = ?, title_page_generated = ?, updated_at = ? WHERE id = ?",
		pagesScanned, titlePageGenerated, time.Now(), id,
	)
	return err
}

func (s *Store) SetPaperlessDocID(id string, docID int) error {
	_, err := s.db.Exec(
		"UPDATE jobs SET paperless_doc_id = ?, updated_at = ? WHERE id = ?",
		docID, time.Now(), id,
	)
	return err
}

func (s *Store) GetState(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)",
		key, value, time.Now(),
	)
	return err
}

func (s *Store) GetLastScanWeek() (string, error) {
	return s.GetState("last_scan_week")
}

func (s *Store) SetLastScanWeek(week string) error {
	return s.SetState("last_scan_week", week)
}

func (s *Store) ListJobs(limit int) ([]*Job, error) {
	rows, err := s.db.Query(
		"SELECT id, status, error, pages_scanned, title_page_generated, paperless_doc_id, created_at, updated_at FROM jobs ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		var titlePageGenerated int
		var paperlessDocID sql.NullInt64
		var createdAt, updatedAt string

		err := rows.Scan(&job.ID, &job.Status, &job.Error, &job.PagesScanned, &titlePageGenerated, &paperlessDocID, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		job.TitlePageGenerated = titlePageGenerated == 1
		if paperlessDocID.Valid {
			job.PaperlessDocID = int(paperlessDocID.Int64)
		}
		job.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		job.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

		jobs = append(jobs, job)
	}

	return jobs, nil
}
