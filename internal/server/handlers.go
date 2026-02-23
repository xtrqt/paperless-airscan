package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xtrqt/paperless-airscan/internal/config"
	"github.com/xtrqt/paperless-airscan/internal/filing"
	"github.com/xtrqt/paperless-airscan/internal/pdf"
	"github.com/xtrqt/paperless-airscan/internal/printer"
	"github.com/xtrqt/paperless-airscan/internal/store"
)

type Handlers struct {
	store   *store.Store
	logger  *slog.Logger
	jobs    chan string
	filing  *filing.Manager
	printer *printer.IPPClient
	cfg     *config.Config
}

func NewHandlers(store *store.Store, logger *slog.Logger, jobs chan string, filing *filing.Manager, printer *printer.IPPClient, cfg *config.Config) *Handlers {
	return &Handlers{
		store:   store,
		logger:  logger,
		jobs:    jobs,
		filing:  filing,
		printer: printer,
		cfg:     cfg,
	}
}

type ScanResponse struct {
	JobID string `json:"job_id"`
}

func (h *Handlers) Scan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobID := uuid.New().String()

	if err := h.store.CreateJob(jobID); err != nil {
		h.logger.Error("failed to create job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	select {
	case h.jobs <- jobID:
		h.logger.Info("job queued", "job_id", jobID)
		writeJSON(w, http.StatusAccepted, ScanResponse{JobID: jobID})
	default:
		h.logger.Warn("job queue full", "job_id", jobID)
		writeError(w, http.StatusServiceUnavailable, "job queue full, try again later")
	}
}

func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobID := strings.TrimPrefix(r.URL.Path, "/status/")
	if jobID == "" || jobID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "job id required")
		return
	}

	job, err := h.store.GetJob(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
	})
}

func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobs, err := h.store.ListJobs(20)
	if err != nil {
		h.logger.Error("failed to list jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	if jobs == nil {
		jobs = []*store.Job{}
	}

	writeJSON(w, http.StatusOK, jobs)
}

// Filing endpoints

type FilingCurrentResponse struct {
	FilingID            string `json:"filing_id,omitempty"`
	CumulativePages     int    `json:"cumulative_pages"`
	TitlePagePrinted    bool   `json:"title_page_printed"`
	Threshold           int    `json:"threshold"`
	PagesUntilThreshold int    `json:"pages_until_threshold"`
	CreatedAt           string `json:"created_at,omitempty"`
}

func (h *Handlers) FilingCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.cfg.Filing.Enabled {
		writeError(w, http.StatusNotImplemented, "filing system not enabled")
		return
	}

	ctx := context.Background()
	batch, err := h.filing.GetCurrentBatch(ctx)
	if err != nil {
		h.logger.Error("failed to get current batch", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get current filing batch")
		return
	}

	if batch == nil {
		writeJSON(w, http.StatusOK, FilingCurrentResponse{
			CumulativePages:     0,
			TitlePagePrinted:    true,
			Threshold:           h.cfg.Filing.PageThreshold,
			PagesUntilThreshold: h.cfg.Filing.PageThreshold,
		})
		return
	}

	pagesUntil := h.cfg.Filing.PageThreshold - batch.CumulativePages
	if pagesUntil < 0 {
		pagesUntil = 0
	}

	writeJSON(w, http.StatusOK, FilingCurrentResponse{
		FilingID:            batch.FilingID,
		CumulativePages:     batch.CumulativePages,
		TitlePagePrinted:    batch.TitlePagePrinted,
		Threshold:           h.cfg.Filing.PageThreshold,
		PagesUntilThreshold: pagesUntil,
		CreatedAt:           batch.CreatedAt.Format(time.RFC3339),
	})
}

type FilingErrorsResponse struct {
	Errors []FilingErrorEntry `json:"errors"`
}

type FilingErrorEntry struct {
	FilingID        string `json:"filing_id"`
	CumulativePages int    `json:"cumulative_pages"`
	PrintError      string `json:"print_error"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func (h *Handlers) FilingErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.cfg.Filing.Enabled {
		writeError(w, http.StatusNotImplemented, "filing system not enabled")
		return
	}

	batches, err := h.store.ListFilingBatchesWithErrors()
	if err != nil {
		h.logger.Error("failed to list filing errors", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list filing errors")
		return
	}

	errors := make([]FilingErrorEntry, 0, len(batches))
	for _, batch := range batches {
		errors = append(errors, FilingErrorEntry{
			FilingID:        batch.FilingID,
			CumulativePages: batch.CumulativePages,
			PrintError:      batch.PrintError,
			CreatedAt:       batch.CreatedAt.Format(time.RFC3339),
			UpdatedAt:       batch.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, FilingErrorsResponse{Errors: errors})
}

type FilingRetryPrintResponse struct {
	Success  bool   `json:"success"`
	FilingID string `json:"filing_id"`
	Message  string `json:"message"`
}

func (h *Handlers) FilingRetryPrint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.cfg.Filing.Enabled {
		writeError(w, http.StatusNotImplemented, "filing system not enabled")
		return
	}

	filingID := strings.TrimPrefix(r.URL.Path, "/filing/print/")
	if filingID == "" || filingID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "filing ID required")
		return
	}

	ctx := context.Background()

	// Get or create filing batch
	batch, err := h.store.GetOrCreateFilingBatch(filingID)
	if err != nil {
		h.logger.Error("failed to get filing batch", "error", err, "filing_id", filingID)
		writeError(w, http.StatusInternalServerError, "failed to get filing batch")
		return
	}

	// Always allow reprinting (manual trigger)
	h.logger.Info("manual title page print requested", "filing_id", filingID, "already_printed", batch.TitlePagePrinted)

	// Generate title page
	titlePDF, err := pdf.GenerateFilingTitlePage(filingID)
	if err != nil {
		h.logger.Error("failed to generate title page", "error", err, "filing_id", filingID)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate title page: %v", err))
		return
	}

	// Print title page
	printerHost := h.cfg.Filing.PrinterHost
	if printerHost == "" {
		printerHost = h.cfg.Scanner.Host
	}

	jobName := fmt.Sprintf("Filing Title Page - %s", filingID)
	err = h.printer.PrintPDF(ctx, printerHost, titlePDF, jobName)
	if err != nil {
		h.logger.Error("failed to print title page", "error", err, "filing_id", filingID)
		h.filing.MarkTitlePagePrinted(ctx, filingID, false, err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to print: %v", err))
		return
	}

	// Mark as printed
	if err := h.filing.MarkTitlePagePrinted(ctx, filingID, true, nil); err != nil {
		h.logger.Error("failed to mark title page as printed", "error", err, "filing_id", filingID)
		// Continue, print was successful
	}

	writeJSON(w, http.StatusOK, FilingRetryPrintResponse{
		Success:  true,
		FilingID: filingID,
		Message:  "Title page printed successfully",
	})
}

func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(lrw, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", lrw.statusCode,
				"duration", time.Since(start),
			)
		})
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered", "error", err, "path", r.URL.Path)
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func JSONContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
