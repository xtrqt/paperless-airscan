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
	"github.com/xtrqt/paperless-airscan/internal/paperless"
	"github.com/xtrqt/paperless-airscan/internal/pdf"
	"github.com/xtrqt/paperless-airscan/internal/scanner"
	"github.com/xtrqt/paperless-airscan/internal/store"
)

type Server struct {
	cfg       *config.Config
	store     *store.Store
	scanner   scanner.Scanner
	paperless *paperless.Client
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

	jobs := make(chan string, 10)

	return &Server{
		cfg:       cfg,
		store:     store,
		scanner:   scan,
		paperless: paperlessClient,
		logger:    logger,
		jobs:      jobs,
	}
}

func (s *Server) Start() error {
	handlers := NewHandlers(s.store, s.logger, s.jobs)

	mux := http.NewServeMux()
	mux.HandleFunc("/scan", handlers.Scan)
	mux.HandleFunc("/status/", handlers.Status)
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/jobs", handlers.ListJobs)

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
	logger := s.logger.With("job_id", jobID)
	logger.Info("processing job")

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

	titlePageGenerated := false
	if s.cfg.TitlePage.Enabled {
		if shouldGenerate, weekKey := s.shouldGenerateTitlePage(); shouldGenerate {
			logger.Info("generating title page", "week", weekKey)

			titlePagePath := filepath.Join(result.TempDir, "title.pdf")
			titleData, err := s.generateTitlePage()
			if err != nil {
				logger.Warn("failed to generate title page, skipping", "error", err)
			} else {
				if err := pdf.BytesToPDF(titleData, titlePagePath); err != nil {
					logger.Warn("failed to write title page", "error", err)
				} else {
					if err := pdf.PrependPage(pdfPath, titlePagePath); err != nil {
						logger.Warn("failed to prepend title page", "error", err)
					} else {
						titlePageGenerated = true
						if err := s.store.SetLastScanWeek(weekKey); err != nil {
							logger.Warn("failed to update last scan week", "error", err)
						}
						logger.Info("title page generated and prepended")
					}
				}
			}
		}
	}

	if err := s.store.UpdateJobProgress(jobID, result.Count, titlePageGenerated); err != nil {
		logger.Error("failed to update job progress", "error", err)
	}

	if err := s.store.UpdateJobStatus(jobID, store.StatusUploading, ""); err != nil {
		logger.Error("failed to update job status", "error", err)
		return
	}

	title := fmt.Sprintf("Scan %s", time.Now().Format("2006-01-02 15:04"))
	taskID, err := s.paperless.UploadDocument(pdfPath, paperless.DocumentMeta{
		Title:   title,
		Created: time.Now(),
	})
	if err != nil {
		s.failJob(jobID, "failed to upload to paperless", err)
		return
	}

	logger.Info("uploaded to paperless", "task_id", taskID)

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

func (s *Server) shouldGenerateTitlePage() (bool, string) {
	now := time.Now()
	currentWeek := pdf.FormatWeekKey(now)

	lastWeek, err := s.store.GetLastScanWeek()
	if err != nil {
		s.logger.Warn("failed to get last scan week", "error", err)
		return true, currentWeek
	}

	if lastWeek == "" {
		return true, currentWeek
	}

	return currentWeek > lastWeek, currentWeek
}

func (s *Server) generateTitlePage() ([]byte, error) {
	now := time.Now()
	weekStartsMonday := s.cfg.TitlePage.WeekStart == "monday"

	config := pdf.GetWeekBounds(now, weekStartsMonday)
	return pdf.GenerateTitlePage(config)
}
