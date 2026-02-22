package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xtrqt/paperless-airscan/internal/store"
)

type Handlers struct {
	store  *store.Store
	logger *slog.Logger
	jobs   chan string
}

func NewHandlers(store *store.Store, logger *slog.Logger, jobs chan string) *Handlers {
	return &Handlers{
		store:  store,
		logger: logger,
		jobs:   jobs,
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
