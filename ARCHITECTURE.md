# Paperless-AirScan Architecture

A Go service that scans documents via AirPrint/eSCL protocol and uploads them to Paperless-ngx.

## Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Paperless-AirScan Service                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  POST /scan ──┬──► Job Queue (SQLite) ──► Scanner Worker           │
│               │                    │           │                    │
│  GET /status/{id} ◄────────────────┘           ▼                    │
│                                          airscan library            │
│                                         (eSCL protocol)             │
│                                               │                     │
│  GET /health                                ▼                       │
│                                     Scan Pages (JPEG/PDF)           │
│                                               │                     │
│                                               ▼                     │
│                                    ┌─────────────────┐              │
│                                    │ Title Page Gen  │◄─ Weekly?    │
│                                    │ (if new week)   │  SQLite      │
│                                    └────────┬────────┘              │
│                                             │                       │
│                                             ▼                       │
│                                    PDF Merge (pdfcpu)               │
│                                             │                       │
│                                             ▼                       │
│                                    Paperless-ngx API                │
│                                             │                       │
│                                             ▼                       │
│                                    Update Job Status                │
└─────────────────────────────────────────────────────────────────────┘
```

## Project Structure

```
paperless-airscan/
├── cmd/paperless-airscan/
│   └── main.go                 # Entry point, starts server
├── internal/
│   ├── config/
│   │   └── config.go           # Env var parsing
│   ├── scanner/
│   │   ├── scanner.go          # Scanner interface
│   │   └── airscan.go          # airscan library wrapper
│   ├── pdf/
│   │   ├── pdf.go              # PDF operations using pdfcpu
│   │   └── titlepage.go        # Weekly title page generation
│   ├── paperless/
│   │   └── client.go           # Paperless-ngx REST API client
│   ├── store/
│   │   ├── sqlite.go           # SQLite database for jobs & week tracking
│   │   └── models.go           # Job model
│   └── server/
│       ├── server.go           # HTTP server setup
│       ├── handlers.go         # /scan, /status/{id}, /health
│       └── middleware.go       # Logging, recovery
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/stapelberg/airscan` | eSCL/AirPrint scanning |
| `github.com/pdfcpu/pdfcpu/pkg/api` | PDF manipulation |
| `github.com/pdfcpu/pdfcpu/pkg/pdfcpu` | PDF operations |
| `modernc.org/sqlite` | Pure Go SQLite driver |
| `github.com/google/uuid` | Job IDs |

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/scan` | Trigger new scan job, returns `{job_id}` |
| GET | `/status/{id}` | Get job status: pending/scanning/processing/uploading/completed/failed |
| GET | `/health` | Health check |

### POST /scan Response

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### GET /status/{id} Response

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "pages_scanned": 5,
  "title_page_generated": true,
  "created_at": "2026-02-22T10:30:00Z",
  "updated_at": "2026-02-22T10:31:30Z",
  "error": null
}
```

## Configuration

Environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `SCANNER_HOST` | Scanner hostname/IP (empty = auto-discover) | `""` |
| `SCANNER_TIMEOUT` | Discovery/scan timeout | `30s` |
| `SCANNER_DUPLEX` | Enable duplex (double-sided) scanning | `true` |
| `SCANNER_SOURCE` | Source: `adf` or `flatbed` | `adf` |
| `SCANNER_COLOR` | Color mode: `grayscale` or `rgb24` | `grayscale` |
| `PAPERLESS_URL` | Paperless-ngx base URL | Required |
| `PAPERLESS_TOKEN` | Paperless-ngx API token | Required |
| `SERVER_ADDR` | Server listen address | `:8080` |
| `TITLE_PAGE_ENABLED` | Generate weekly title pages | `true` |
| `TITLE_PAGE_WEEK_START` | Week start: `monday` or `sunday` | `monday` |
| `DATABASE_PATH` | SQLite database path | `/data/paperless-airscan.db` |
| `TEMP_DIR` | Temporary directory for scans | `/tmp/paperless-airscan` |

## Database Schema

```sql
-- Jobs table
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    error TEXT,
    pages_scanned INTEGER DEFAULT 0,
    title_page_generated INTEGER DEFAULT 0,
    paperless_doc_id INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- State table for tracking
CREATE TABLE state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for status queries
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_created ON jobs(created_at);
```

### Job Status Values

| Status | Description |
|--------|-------------|
| `pending` | Job queued, waiting for worker |
| `scanning` | Scanner actively scanning pages |
| `processing` | Processing PDF, generating title page |
| `uploading` | Uploading to Paperless-ngx |
| `completed` | Successfully finished |
| `failed` | Error occurred (see `error` field) |

## Title Page Logic

The service generates a title page for the first scan of each week (ISO week, starting Monday by default).

1. Calculate current ISO week: `year, week := time.Now().ISOWeek()`
2. Query `state` table for `last_scan_week`
3. If `"{year}-W{week:02d}" > last_scan_week`:
   - Generate title page with:
     - "Week {week_number}, {year}"
     - Date range: Monday to Sunday
   - Prepend to PDF
4. Update `last_scan_week` to current week

### Title Page Content

```
┌──────────────────────────────────────┐
│                                      │
│                                      │
│          WEEK 8, 2026                │
│                                      │
│      17 February - 23 February       │
│                                      │
│                                      │
└──────────────────────────────────────┘
```

## Scanner Module

Uses the `github.com/stapelberg/airscan` library for eSCL/AirPrint scanning.

### Discovery

If `SCANNER_HOST` is not set, the service uses mDNS/Bonjour to discover AirScan-compatible scanners on the network.

### Scanning Flow

1. Connect to scanner via eSCL protocol
2. Query scanner capabilities
3. Configure scan job:
   - Source: ADF or flatbed
   - Duplex: enabled for double-sided
   - Color mode: grayscale or RGB24
   - Format: JPEG (converted to PDF later) or PDF directly
4. Retrieve scanned pages
5. Store temporarily for processing

## PDF Module

Uses `github.com/pdfcpu/pdfcpu` for PDF operations.

### Operations

1. Convert JPEG pages to individual PDFs (if needed)
2. Generate title page PDF (if first scan of week)
3. Merge all pages into single PDF
4. Optimize PDF for Paperless-ngx ingestion

## Paperless-ngx Integration

REST API client for document upload.

### Upload Flow

1. POST to `/api/documents/post_document/`
2. Multipart form with:
   - `document`: PDF file
   - `title`: Auto-generated from date
   - `created`: Scan timestamp
   - `tags`: Configurable default tags
3. Returns document ID
4. Store ID in job record

### API Reference

```go
type Client struct {
    baseURL string
    token   string
    client  *http.Client
}

func (c *Client) UploadDocument(ctx context.Context, pdfPath string, meta DocumentMeta) (int, error)
func (c *Client) GetDocument(ctx context.Context, id int) (*Document, error)
```

## Server Module

Simple HTTP server with async job processing.

### Architecture

```
                    ┌─────────────────┐
   HTTP Request ───►│  HTTP Handler   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   Job Queue     │
                    │   (SQLite)      │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Worker Pool    │◄── single worker for now
                    │  (goroutine)    │
                    └─────────────────┘
```

### Concurrency

- Single worker goroutine processes jobs sequentially
- Prevents scanner conflicts
- Status queries are non-blocking

## Docker Deployment

### Dockerfile

Multi-stage build with:
- Builder stage: Go compilation
- Runtime stage: Minimal Alpine with CA certificates

### docker-compose.yml

```yaml
version: "3.8"
services:
  paperless-airscan:
    build: .
    ports:
      - "8080:8080"
    environment:
      - PAPERLESS_URL=http://paperless:8000
      - PAPERLESS_TOKEN=${PAPERLESS_TOKEN}
    volumes:
      - paperless-airscan-data:/data
    depends_on:
      - paperless

  paperless:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    # ... paperless configuration

volumes:
  paperless-airscan-data:
```

## Error Handling

| Error Type | Handling |
|------------|----------|
| Scanner not found | Job fails with error, retry via new request |
| Scanner busy | Job fails, client should retry |
| Paperless unavailable | Job fails with error, document retained locally |
| PDF processing error | Job fails, raw scans retained for recovery |

## Logging

Structured JSON logging with levels:
- `DEBUG`: Detailed scan operations
- `INFO`: Job lifecycle events
- `WARN`: Recoverable errors
- `ERROR`: Job failures

## Future Enhancements

Potential improvements not in initial scope:

- Multiple worker support with scanner locking
- Web UI for job monitoring
- Direct scan button integration (watch scanner button)
- OCR preprocessing options
- Multiple Paperless-ngx instances
- Document tagging rules
- Archive organization patterns
