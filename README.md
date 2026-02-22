# Paperless-AirScan

A Go service that scans documents from AirPrint/eSCL-compatible scanners and uploads them to [Paperless-ngx](https://github.com/paperless-ngx/paperless-ngx).

## Features

- **AirPrint/eSCL Protocol**: Works with any AirPrint-compatible scanner (HP, Brother, Canon, Epson, etc.)
- **Duplex Scanning**: Automatic double-sided scanning via ADF
- **Weekly Title Pages**: Automatically generates a title page for the first scan of each week (useful for archiving)
- **Async Processing**: Webhook triggers scan job, poll for status
- **Paperless-ngx Integration**: Direct upload to your Paperless instance
- **Docker Ready**: Includes Dockerfile and docker-compose.yml

## Quick Start

### 1. Clone and Configure

```bash
git clone https://github.com/xtrqt/paperless-airscan.git
cd paperless-airscan
cp .env.example .env
# Edit .env with your settings
```

### 2. Run with Docker Compose

```bash
docker compose up -d
```

### 3. Trigger a Scan

```bash
curl -X POST http://localhost:8080/scan
# Returns: {"job_id":"550e8400-e29b-41d4-a716-446655440000"}
```

### 4. Check Status

```bash
curl http://localhost:8080/status/550e8400-e29b-41d4-a716-446655440000
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/scan` | Trigger a new scan job |
| GET | `/status/{id}` | Get job status |
| GET | `/jobs` | List recent jobs |
| GET | `/health` | Health check |

### Job Status Values

| Status | Description |
|--------|-------------|
| `pending` | Job queued |
| `scanning` | Scanner active |
| `processing` | Processing PDF |
| `uploading` | Uploading to Paperless |
| `completed` | Done |
| `failed` | Error occurred |

## Configuration

All configuration via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `SCANNER_HOST` | Scanner hostname/IP (empty = auto-discover) | `""` |
| `SCANNER_TIMEOUT` | Discovery timeout | `30s` |
| `SCANNER_DUPLEX` | Enable duplex scanning | `true` |
| `SCANNER_SOURCE` | `adf` or `flatbed` | `adf` |
| `SCANNER_COLOR` | `grayscale` or `rgb24` | `grayscale` |
| `SCANNER_REORDER` | `interleave` to reorder duplex pages (fronts-then-backs → interleaved) | `""` |
| `PAPERLESS_URL` | Paperless-ngx URL | Required |
| `PAPERLESS_TOKEN` | API token | Required |
| `SERVER_ADDR` | Listen address | `:8080` |
| `TITLE_PAGE_ENABLED` | Generate weekly title pages | `true` |
| `TITLE_PAGE_WEEK_START` | `monday` or `sunday` | `monday` |
| `DATABASE_PATH` | SQLite database path | `/data/paperless-airscan.db` |

### Duplex Page Reordering

Some HP scanners return duplex scan pages in "fronts-then-backs" order:
- Received: `front1, front2, front3, back3, back2, back1` (or `back1, back2, back3`)

Set `SCANNER_REORDER=interleave` to reorder pages into reading order:
- Result: `front1, back1, front2, back2, front3, back3`

## Getting Paperless-ngx API Token

1. Log into your Paperless-ngx instance
2. Go to Settings → Administration → Auth Tokens
3. Create a new token with appropriate permissions

## Webhook Integration

Use with any webhook service (n8n, Zapier, Home Assistant, etc.):

```yaml
# n8n example
- Webhook node → HTTP Request node to POST /scan
- Wait node → HTTP Request node to GET /status/{id}
- Continue when status == "completed"
```

## Building from Source

```bash
go build -o paperless-airscan ./cmd/paperless-airscan
```

## Architecture

See [ARCHITECTURE.md](./ARCHITECTURE.md) for detailed architecture documentation.

## Tested Scanners

Based on the [airscan library](https://github.com/stapelberg/airscan#tested-devices):

- HP OfficeJet Pro 9010 series
- HP Laserjet M479fdw
- Brother MFC-L2750DW
- Brother MFC-L2710DN
- Canon G3560
- Epson XP-7100

If your scanner works with Apple AirPrint, it should work with this service.

## License

MIT
