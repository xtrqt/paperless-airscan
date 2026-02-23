package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

func New(databasePath string, logger *slog.Logger) (*Store, error) {
	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		logger: logger,
	}

	if err := store.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
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
	var filingID sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		"SELECT id, status, error, pages_scanned, title_page_generated, paperless_doc_id, filing_id, created_at, updated_at FROM jobs WHERE id = ?",
		id,
	).Scan(&job.ID, &job.Status, &job.Error, &job.PagesScanned, &titlePageGenerated, &paperlessDocID, &filingID, &createdAt, &updatedAt)

	if err != nil {
		return nil, err
	}

	job.TitlePageGenerated = titlePageGenerated == 1
	if paperlessDocID.Valid {
		job.PaperlessDocID = int(paperlessDocID.Int64)
	}
	if filingID.Valid {
		job.FilingID = filingID.String
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

func (s *Store) UpdateJobFilingID(id string, filingID string) error {
	_, err := s.db.Exec(
		"UPDATE jobs SET filing_id = ?, updated_at = ? WHERE id = ?",
		filingID, time.Now(), id,
	)
	return err
}

func (s *Store) ListJobs(limit int) ([]*Job, error) {
	rows, err := s.db.Query(
		"SELECT id, status, error, pages_scanned, title_page_generated, paperless_doc_id, filing_id, created_at, updated_at FROM jobs ORDER BY created_at DESC LIMIT ?",
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
		var filingID sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(&job.ID, &job.Status, &job.Error, &job.PagesScanned, &titlePageGenerated, &paperlessDocID, &filingID, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		job.TitlePageGenerated = titlePageGenerated == 1
		if paperlessDocID.Valid {
			job.PaperlessDocID = int(paperlessDocID.Int64)
		}
		if filingID.Valid {
			job.FilingID = filingID.String
		}
		job.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		job.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Filing batch methods

func (s *Store) GetOrCreateFilingBatch(filingID string) (*FilingBatch, error) {
	batch := &FilingBatch{}
	var titlePagePrinted int
	var printError sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		"SELECT filing_id, cumulative_pages, title_page_printed, print_error, created_at, updated_at FROM filing_batches WHERE filing_id = ?",
		filingID,
	).Scan(&batch.FilingID, &batch.CumulativePages, &titlePagePrinted, &printError, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		// Create new filing batch (title_page_printed = 0 because we haven't printed it yet)
		_, err := s.db.Exec(
			"INSERT INTO filing_batches (filing_id, cumulative_pages, title_page_printed, created_at, updated_at) VALUES (?, 0, 0, ?, ?)",
			filingID, time.Now(), time.Now(),
		)
		if err != nil {
			return nil, err
		}
		return &FilingBatch{
			FilingID:         filingID,
			CumulativePages:  0,
			TitlePagePrinted: false,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}, nil
	}

	if err != nil {
		return nil, err
	}

	batch.TitlePagePrinted = titlePagePrinted == 1
	if printError.Valid {
		batch.PrintError = printError.String
	}
	batch.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	batch.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return batch, nil
}

func (s *Store) UpdateFilingBatchPages(filingID string, additionalPages int) error {
	_, err := s.db.Exec(
		"UPDATE filing_batches SET cumulative_pages = cumulative_pages + ?, updated_at = ? WHERE filing_id = ?",
		additionalPages, time.Now(), filingID,
	)
	return err
}

func (s *Store) MarkTitlePagePrinted(filingID string, success bool, errorMsg string) error {
	printed := 0
	if success {
		printed = 1
	}
	_, err := s.db.Exec(
		"UPDATE filing_batches SET title_page_printed = ?, print_error = ?, updated_at = ? WHERE filing_id = ?",
		printed, errorMsg, time.Now(), filingID,
	)
	return err
}

func (s *Store) GetPendingPrintFilingBatch() (*FilingBatch, error) {
	batch := &FilingBatch{}
	var titlePagePrinted int
	var printError sql.NullString
	var createdAt, updatedAt string

	// Only find batches where print was attempted and failed (has error message)
	// New batches with title_page_printed=0 but no print_error should NOT block scans
	err := s.db.QueryRow(
		"SELECT filing_id, cumulative_pages, title_page_printed, print_error, created_at, updated_at FROM filing_batches WHERE title_page_printed = 0 AND print_error IS NOT NULL AND print_error != '' ORDER BY updated_at ASC LIMIT 1",
	).Scan(&batch.FilingID, &batch.CumulativePages, &titlePagePrinted, &printError, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	batch.TitlePagePrinted = titlePagePrinted == 1
	if printError.Valid {
		batch.PrintError = printError.String
	}
	batch.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	batch.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return batch, nil
}

func (s *Store) GetLatestFilingBatchForDate(datePrefix string) (*FilingBatch, error) {
	batch := &FilingBatch{}
	var titlePagePrinted int
	var printError sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		"SELECT filing_id, cumulative_pages, title_page_printed, print_error, created_at, updated_at FROM filing_batches WHERE filing_id LIKE ? ORDER BY filing_id DESC LIMIT 1",
		datePrefix+"%",
	).Scan(&batch.FilingID, &batch.CumulativePages, &titlePagePrinted, &printError, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	batch.TitlePagePrinted = titlePagePrinted == 1
	if printError.Valid {
		batch.PrintError = printError.String
	}
	batch.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	batch.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return batch, nil
}

func (s *Store) ListFilingBatchesWithErrors() ([]*FilingBatch, error) {
	rows, err := s.db.Query(
		"SELECT filing_id, cumulative_pages, title_page_printed, print_error, created_at, updated_at FROM filing_batches WHERE title_page_printed = 0 ORDER BY updated_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var batches []*FilingBatch
	for rows.Next() {
		batch := &FilingBatch{}
		var titlePagePrinted int
		var printError sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(&batch.FilingID, &batch.CumulativePages, &titlePagePrinted, &printError, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		batch.TitlePagePrinted = titlePagePrinted == 1
		if printError.Valid {
			batch.PrintError = printError.String
		}
		batch.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		batch.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

		batches = append(batches, batch)
	}

	return batches, nil
}
