package store

import "time"

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusScanning   JobStatus = "scanning"
	StatusProcessing JobStatus = "processing"
	StatusUploading  JobStatus = "uploading"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

type Job struct {
	ID                 string    `json:"id"`
	Status             JobStatus `json:"status"`
	Error              string    `json:"error,omitempty"`
	PagesScanned       int       `json:"pages_scanned"`
	TitlePageGenerated bool      `json:"title_page_generated"`
	PaperlessDocID     int       `json:"paperless_doc_id,omitempty"`
	FilingID           string    `json:"filing_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type State struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FilingBatch struct {
	FilingID         string    `json:"filing_id"`
	CumulativePages  int       `json:"cumulative_pages"`
	TitlePagePrinted bool      `json:"title_page_printed"`
	PrintError       string    `json:"print_error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
