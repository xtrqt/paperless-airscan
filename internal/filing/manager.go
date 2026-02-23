package filing

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/xtrqt/paperless-airscan/internal/store"
)

type Manager struct {
	store     *store.Store
	threshold int
	logger    *slog.Logger
}

func NewManager(store *store.Store, threshold int, logger *slog.Logger) *Manager {
	return &Manager{
		store:     store,
		threshold: threshold,
		logger:    logger,
	}
}

// GetOrCreateFilingID returns current filing ID or creates new one
func (m *Manager) GetOrCreateFilingID(ctx context.Context) (string, error) {
	// Use UTC for filing IDs
	now := time.Now().UTC()
	datePrefix := now.Format("20060102") // YYYYMMDD

	// Get latest filing batch for today
	batch, err := m.store.GetLatestFilingBatchForDate(datePrefix)
	if err != nil {
		return "", fmt.Errorf("get latest filing batch: %w", err)
	}

	// If no batch exists for today, or the existing batch has exceeded threshold and title page is printed,
	// create a new filing ID
	if batch == nil {
		newFilingID := datePrefix + "00"
		m.logger.Info("creating new filing ID", "filing_id", newFilingID, "reason", "no batch for date")
		_, err := m.store.GetOrCreateFilingBatch(newFilingID)
		if err != nil {
			return "", fmt.Errorf("create filing batch: %w", err)
		}
		return newFilingID, nil
	}

	// Check if batch exceeded threshold and title page is printed (or pending)
	if batch.CumulativePages >= m.threshold {
		if batch.TitlePagePrinted {
			// Title page already printed, create next filing ID
			newFilingID, err := m.GenerateFilingID(now)
			if err != nil {
				return "", fmt.Errorf("generate next filing ID: %w", err)
			}
			m.logger.Info("creating new filing ID", "filing_id", newFilingID, "reason", "threshold_exceeded_and_printed", "previous_id", batch.FilingID)
			_, err = m.store.GetOrCreateFilingBatch(newFilingID)
			if err != nil {
				return "", fmt.Errorf("create filing batch: %w", err)
			}
			return newFilingID, nil
		} else {
			// Title page print is pending - reuse existing ID
			m.logger.Info("reusing filing ID", "filing_id", batch.FilingID, "reason", "print_pending")
			return batch.FilingID, nil
		}
	}

	// Threshold not exceeded, reuse existing filing ID
	m.logger.Info("reusing filing ID", "filing_id", batch.FilingID, "cumulative_pages", batch.CumulativePages)
	return batch.FilingID, nil
}

// IncrementPageCount adds pages to current filing batch after successful upload
func (m *Manager) IncrementPageCount(ctx context.Context, filingID string, pages int) error {
	err := m.store.UpdateFilingBatchPages(filingID, pages)
	if err != nil {
		return fmt.Errorf("update filing batch pages: %w", err)
	}
	m.logger.Info("incremented page count", "filing_id", filingID, "additional_pages", pages)
	return nil
}

// CheckThreshold returns true if current batch exceeds threshold
func (m *Manager) CheckThreshold(ctx context.Context, filingID string) (bool, error) {
	batch, err := m.store.GetOrCreateFilingBatch(filingID)
	if err != nil {
		return false, fmt.Errorf("get filing batch: %w", err)
	}

	exceeded := batch.CumulativePages >= m.threshold
	if exceeded {
		m.logger.Info("threshold exceeded", "filing_id", filingID, "cumulative_pages", batch.CumulativePages, "threshold", m.threshold)
	}
	return exceeded, nil
}

// GenerateFilingID creates new ID in YYYYMMDDII format
// Finds the highest index for the given date and increments it
func (m *Manager) GenerateFilingID(date time.Time) (string, error) {
	datePrefix := date.UTC().Format("20060102") // YYYYMMDD

	// Get the latest filing batch for this date
	batch, err := m.store.GetLatestFilingBatchForDate(datePrefix)
	if err != nil {
		return "", fmt.Errorf("get latest filing batch: %w", err)
	}

	if batch == nil {
		// No batches for this date, start with 00
		return datePrefix + "00", nil
	}

	// Extract index from filing ID (last 2+ characters)
	if len(batch.FilingID) < 10 {
		return "", fmt.Errorf("invalid filing ID format: %s", batch.FilingID)
	}

	indexStr := batch.FilingID[8:] // Everything after YYYYMMDD
	currentIndex, err := strconv.Atoi(indexStr)
	if err != nil {
		return "", fmt.Errorf("parse filing ID index: %w", err)
	}

	// Increment index (support unlimited increments)
	newIndex := currentIndex + 1

	// Format with leading zeros (minimum 2 digits, but can grow: 00, 01...99, 100, 101...)
	var newFilingID string
	if newIndex < 100 {
		newFilingID = fmt.Sprintf("%s%02d", datePrefix, newIndex)
	} else {
		newFilingID = fmt.Sprintf("%s%d", datePrefix, newIndex)
	}

	m.logger.Info("generated new filing ID", "filing_id", newFilingID, "previous_id", batch.FilingID, "index", newIndex)
	return newFilingID, nil
}

// MarkTitlePagePrinted updates print status
func (m *Manager) MarkTitlePagePrinted(ctx context.Context, filingID string, success bool, printErr error) error {
	errMsg := ""
	if printErr != nil {
		errMsg = printErr.Error()
	}

	err := m.store.MarkTitlePagePrinted(filingID, success, errMsg)
	if err != nil {
		return fmt.Errorf("mark title page printed: %w", err)
	}

	if success {
		m.logger.Info("marked title page as printed", "filing_id", filingID)
	} else {
		m.logger.Warn("marked title page print as failed", "filing_id", filingID, "error", errMsg)
	}

	return nil
}

// GetCurrentBatch returns the current active filing batch
func (m *Manager) GetCurrentBatch(ctx context.Context) (*store.FilingBatch, error) {
	now := time.Now().UTC()
	datePrefix := now.Format("20060102")

	batch, err := m.store.GetLatestFilingBatchForDate(datePrefix)
	if err != nil {
		return nil, fmt.Errorf("get latest filing batch: %w", err)
	}

	return batch, nil
}
