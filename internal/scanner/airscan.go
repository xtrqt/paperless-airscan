package scanner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/brutella/dnssd"
	"github.com/stapelberg/airscan"
	"github.com/stapelberg/airscan/preset"
)

type AirScanScanner struct {
	host    string
	timeout time.Duration
	duplex  bool
	source  Source
	color   ColorMode
	logger  *slog.Logger
}

func NewAirScanScanner(host string, timeout time.Duration, duplex bool, source Source, color ColorMode, logger *slog.Logger) *AirScanScanner {
	return &AirScanScanner{
		host:    host,
		timeout: timeout,
		duplex:  duplex,
		source:  source,
		color:   color,
		logger:  logger,
	}
}

func (s *AirScanScanner) discoverService(ctx context.Context) (*dnssd.BrowseEntry, error) {
	s.logger.Info("discovering scanner via mDNS", "timeout", s.timeout)

	var foundService *dnssd.BrowseEntry

	addFn := func(service dnssd.BrowseEntry) {
		humanName := service.Text["ty"]
		if humanName == "" {
			humanName = service.Name
		}

		if s.host != "" && s.host == service.Host {
			s.logger.Info("found scanner by host", "name", humanName, "host", service.Host)
			foundService = &service
			return
		}

		s.logger.Info("discovered scanner", "name", humanName, "host", service.Host)
		if foundService == nil {
			foundService = &service
		}
	}

	rmvFn := func(service dnssd.BrowseEntry) {
		s.logger.Debug("scanner removed from discovery", "name", service.Name)
	}

	if err := dnssd.LookupType(ctx, airscan.ServiceName, addFn, rmvFn); err != nil {
		if err != context.Canceled && err != context.DeadlineExceeded {
			return nil, fmt.Errorf("mDNS lookup failed: %w", err)
		}
	}

	if foundService == nil {
		return nil, fmt.Errorf("no airscan-compatible scanners found")
	}

	return foundService, nil
}

func (s *AirScanScanner) createClient(ctx context.Context) (*airscan.Client, error) {
	if s.host != "" {
		s.logger.Info("connecting to scanner by host", "host", s.host)
		return airscan.NewClient(s.host), nil
	}

	service, err := s.discoverService(ctx)
	if err != nil {
		return nil, err
	}

	return airscan.NewClientForService(service), nil
}

func (s *AirScanScanner) buildScanSettings() *airscan.ScanSettings {
	settings := preset.GrayscaleA4ADF()

	switch s.source {
	case SourceFlatbed:
		settings.InputSource = "Platen"
	case SourceADF:
		settings.InputSource = "Feeder"
	}

	switch s.color {
	case ColorRGB24:
		settings.ColorMode = "RGB24"
	case ColorGrayscale:
		settings.ColorMode = "Grayscale8"
	}

	settings.Duplex = s.duplex
	settings.DocumentFormat = "image/jpeg"

	return settings
}

func (s *AirScanScanner) Scan() (*ScanResult, error) {
	ctx := context.Background()

	client, err := s.createClient(ctx)
	if err != nil {
		return nil, err
	}

	status, err := client.ScannerStatus()
	if err != nil {
		s.logger.Warn("failed to get scanner status", "error", err)
	} else {
		s.logger.Info("scanner status", "state", status.State)
	}

	settings := s.buildScanSettings()

	s.logger.Info("starting scan",
		"source", settings.InputSource,
		"duplex", settings.Duplex,
		"color", settings.ColorMode,
	)

	scanState, err := client.Scan(settings)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	defer scanState.Close()

	tempDir, err := os.MkdirTemp("", "hpscan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	result := &ScanResult{}
	pageIndex := 0

	for scanState.ScanPage() {
		pageIndex++
		pageReader := scanState.CurrentPage()

		pagePath := filepath.Join(tempDir, fmt.Sprintf("page%03d.jpg", pageIndex))
		f, err := os.Create(pagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create page file: %w", err)
		}

		written, err := io.Copy(f, pageReader)
		f.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to write page: %w", err)
		}

		result.Count++
		s.logger.Info("scanned page", "page", pageIndex, "bytes", written, "path", pagePath)
	}

	if err := scanState.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	s.logger.Info("scan complete", "pages", result.Count, "temp_dir", tempDir)
	return result, nil
}

func (s *AirScanScanner) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.createClient(ctx)
	return err == nil
}
