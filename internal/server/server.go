package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xtrqt/paperless-airscan/internal/config"
	"github.com/xtrqt/paperless-airscan/internal/filing"
	"github.com/xtrqt/paperless-airscan/internal/paperless"
	"github.com/xtrqt/paperless-airscan/internal/pdf"
	"github.com/xtrqt/paperless-airscan/internal/printer"
	"github.com/xtrqt/paperless-airscan/internal/scanner"
	"github.com/xtrqt/paperless-airscan/internal/store"
)

type Server struct {
	cfg       *config.Config
	store     *store.Store
	scanner   scanner.Scanner
	paperless *paperless.Client
	filing    *filing.Manager
	printer   *printer.IPPClient
	logger    *slog.Logger
	jobs      chan string
	server    *http.Server
}

func New(cfg *config.Config, store *store.Store, logger *slog.Logger) *Server {
	scan := scanner.NewAirScanScanner(
		cfg.Scanner.Host,
		cfg.Scanner.Timeout,
		cfg.Scanner.Duplex,
		scanner.Source(cfg.Scanner.Source),
		scanner.ColorMode(cfg.Scanner.Color),
		scanner.ReorderMode(cfg.Scanner.Reorder),
		logger,
	)

	paperlessClient := paperless.NewClient(cfg.Paperless.URL, cfg.Paperless.Token)
	filingMgr := filing.NewManager(store, cfg.Filing.PageThreshold, logger)
	printerClient := printer.NewIPPClient(logger)

	jobs := make(chan string, 10)

	return &Server{
		cfg:       cfg,
		store:     store,
		scanner:   scan,
		paperless: paperlessClient,
		filing:    filingMgr,
		printer:   printerClient,
		logger:    logger,
		jobs:      jobs,
	}
}

func (s *Server) Start() error {
	handlers := NewHandlers(s.store, s.logger, s.jobs, s.filing, s.printer, s.cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/scan", handlers.Scan)
	mux.HandleFunc("/status/", handlers.Status)
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/jobs", handlers.ListJobs)

	// Filing endpoints
	mux.HandleFunc("/filing/current", handlers.FilingCurrent)
	mux.HandleFunc("/filing/errors", handlers.FilingErrors)
	mux.HandleFunc("/filing/print/", handlers.FilingRetryPrint)

	handler := LoggingMiddleware(s.logger)(RecoveryMiddleware(s.logger)(mux))

	s.server = &http.Server{
		Addr:         s.cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go s.worker()

	s.logger.Info("starting server", "addr", s.cfg.Server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.jobs)
	return s.server.Shutdown(ctx)
}

func (s *Server) worker() {
	for jobID := range s.jobs {
		s.processJob(jobID)
	}
}

func (s *Server) processJob(jobID string) {
	ctx := context.Background()
	logger := s.logger.With("job_id", jobID)
	logger.Info("processing job")

	// Step 1: Check for pending print jobs (blocking)
	if s.cfg.Filing.Enabled {
		if blocked, err := s.checkPrintBlocking(jobID); blocked || err != nil {
			if err != nil {
				logger.Error("failed to check print blocking", "error", err)
			}
			return
		}
	}

	// Step 2: Get/create filing ID BEFORE scanning
	var filingID string
	if s.cfg.Filing.Enabled {
		var err error
		filingID, err = s.filing.GetOrCreateFilingID(ctx)
		if err != nil {
			s.failJob(jobID, "failed to get filing ID", err)
			return
		}
		logger.Info("assigned filing ID", "filing_id", filingID)

		// Update job with filing ID
		if err := s.store.UpdateJobFilingID(jobID, filingID); err != nil {
			logger.Warn("failed to update job filing ID", "error", err)
		}
	}

	// Step 3: Scan documents
	if err := s.store.UpdateJobStatus(jobID, store.StatusScanning, ""); err != nil {
		logger.Error("failed to update job status", "error", err)
		return
	}

	result, err := s.scanner.Scan()
	if err != nil {
		s.failJob(jobID, "scan failed", err)
		return
	}

	if result.TempDir != "" {
		defer os.RemoveAll(result.TempDir)
	}

	logger.Info("scan complete", "pages", result.Count)

	if err := s.store.UpdateJobProgress(jobID, result.Count, false); err != nil {
		logger.Error("failed to update job progress", "error", err)
	}

	// Step 4: Process PDF
	if err := s.store.UpdateJobStatus(jobID, store.StatusProcessing, ""); err != nil {
		logger.Error("failed to update job status", "error", err)
		return
	}

	jpegFiles := result.FilePaths
	if len(jpegFiles) == 0 {
		s.failJob(jobID, "no pages scanned", nil)
		return
	}

	pdfPath := filepath.Join(result.TempDir, "document.pdf")
	if err := pdf.MergeJPEGsToPDF(pdfPath, jpegFiles); err != nil {
		s.failJob(jobID, "failed to create PDF", err)
		return
	}

	// Step 5: Inject filing metadata into PDF
	if s.cfg.Filing.Enabled && filingID != "" {
		if err := pdf.InjectFilingMetadata(pdfPath, filingID, result.Count, time.Now()); err != nil {
			logger.Warn("failed to inject filing metadata", "error", err, "filing_id", filingID)
			// Continue anyway, not critical
		} else {
			logger.Info("injected filing metadata", "filing_id", filingID)
		}
	}

	// Step 6: Upload to Paperless
	if err := s.store.UpdateJobStatus(jobID, store.StatusUploading, ""); err != nil {
		logger.Error("failed to update job status", "error", err)
		return
	}

	title := fmt.Sprintf("Scan %s", time.Now().Format("2006-01-02 15:04"))

	// Build document metadata
	meta := paperless.DocumentMeta{
		Title:   title,
		Created: time.Now(),
	}

	// Add filing custom fields if filing system is enabled
	if s.cfg.Filing.Enabled && filingID != "" {
		meta.CustomFields = map[string]string{
			"filing_id":  filingID,
			"page_count": fmt.Sprintf("%d", result.Count),
			"scan_date":  time.Now().Format(time.RFC3339),
		}
		logger.Info("adding filing custom fields", "filing_id", filingID, "page_count", result.Count)
	}

	taskID, err := s.paperless.UploadDocument(pdfPath, meta)
	if err != nil {
		s.failJob(jobID, "failed to upload to paperless", err)
		return
	}

	logger.Info("uploaded to paperless", "task_id", taskID)

	// Step 7: Increment page counter AFTER successful upload
	if s.cfg.Filing.Enabled && filingID != "" {
		if err := s.filing.IncrementPageCount(ctx, filingID, result.Count); err != nil {
			logger.Error("failed to increment page count", "error", err, "filing_id", filingID)
			// Continue, don't fail the job
		}

		// Step 8: Check threshold and trigger print if needed
		exceeded, err := s.filing.CheckThreshold(ctx, filingID)
		if err != nil {
			logger.Error("failed to check threshold", "error", err, "filing_id", filingID)
		} else if exceeded {
			logger.Info("threshold exceeded, printing title page", "filing_id", filingID)
			if err := s.printTitlePage(ctx, filingID); err != nil {
				logger.Error("failed to print title page", "filing_id", filingID, "error", err)
				s.filing.MarkTitlePagePrinted(ctx, filingID, false, err)
				// Don't fail the job, but next scan will be blocked
			} else {
				logger.Info("title page printed successfully", "filing_id", filingID)
				s.filing.MarkTitlePagePrinted(ctx, filingID, true, nil)
			}
		}
	}

	// Step 9: Mark job completed
	if err := s.store.UpdateJobStatus(jobID, store.StatusCompleted, ""); err != nil {
		logger.Error("failed to update job status", "error", err)
		return
	}

	logger.Info("job completed successfully")
}

func (s *Server) failJob(jobID, message string, err error) {
	errMsg := message
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", message, err)
	}
	s.logger.Error("job failed", "job_id", jobID, "error", errMsg)
	if err := s.store.UpdateJobStatus(jobID, store.StatusFailed, errMsg); err != nil {
		s.logger.Error("failed to update job status", "error", err)
	}
}

// checkPrintBlocking returns true if scan should be blocked due to pending print
func (s *Server) checkPrintBlocking(jobID string) (bool, error) {
	batch, err := s.store.GetPendingPrintFilingBatch()
	if err != nil {
		return false, err
	}

	if batch != nil && !batch.TitlePagePrinted {
		errMsg := fmt.Sprintf("Print pending for filing ID %s. Use POST /filing/print/%s to retry.", batch.FilingID, batch.FilingID)
		if batch.PrintError != "" {
			errMsg += fmt.Sprintf(" Last error: %s", batch.PrintError)
		}
		s.failJob(jobID, errMsg, nil)
		return true, nil
	}

	return false, nil
}

// printTitlePage generates and prints title page for filing batch
func (s *Server) printTitlePage(ctx context.Context, filingID string) error {
	titlePDF, err := pdf.GenerateFilingTitlePage(filingID)
	if err != nil {
		return fmt.Errorf("generate title page: %w", err)
	}

	printerHost := s.cfg.Filing.PrinterHost
	if printerHost == "" {
		printerHost = s.cfg.Scanner.Host
	}

	jobName := fmt.Sprintf("Filing Title Page - %s", filingID)
	return s.printer.PrintPDF(ctx, printerHost, titlePDF, jobName)
}
