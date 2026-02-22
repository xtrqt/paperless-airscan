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
	reorder ReorderMode
	logger  *slog.Logger
}

func NewAirScanScanner(host string, timeout time.Duration, duplex bool, source Source, color ColorMode, reorder ReorderMode, logger *slog.Logger) *AirScanScanner {
	return &AirScanScanner{
		host:    host,
		timeout: timeout,
		duplex:  duplex,
		source:  source,
		color:   color,
		reorder: reorder,
		logger:  logger,
	}
}

func (s *AirScanScanner) discoverService(ctx context.Context) (*dnssd.BrowseEntry, error) {
	s.logger.Info("discovering scanner via mDNS", "timeout", s.timeout)

	var foundService *dnssd.BrowseEntry
	foundChan := make(chan *dnssd.BrowseEntry, 1)

	addFn := func(service dnssd.BrowseEntry) {
		humanName := service.Text["ty"]
		if humanName == "" {
			humanName = service.Name
		}

		if s.host != "" && s.host == service.Host {
			s.logger.Info("found scanner by host", "name", humanName, "host", service.Host)
			select {
			case foundChan <- &service:
			default:
			}
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

	lookupCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	go func() {
		_ = dnssd.LookupType(lookupCtx, airscan.ServiceName, addFn, rmvFn)
	}()

	select {
	case svc := <-foundChan:
		return svc, nil
	case <-lookupCtx.Done():
		if foundService != nil {
			return foundService, nil
		}
		return nil, fmt.Errorf("no airscan-compatible scanners found within %v", s.timeout)
	}
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

	result := &ScanResult{
		TempDir:   tempDir,
		FilePaths: []string{},
	}

	var rawPaths []string
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

		rawPaths = append(rawPaths, pagePath)
		result.Count++
		s.logger.Info("scanned page", "page", pageIndex, "bytes", written, "path", pagePath)
	}

	if err := scanState.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	if s.reorder == ReorderInterleave && s.duplex && len(rawPaths) > 1 {
		s.logger.Info("reordering pages for duplex scan", "mode", "interleave", "pages", len(rawPaths))
		result.FilePaths = interleavePages(rawPaths, tempDir, s.logger)
	} else {
		result.FilePaths = rawPaths
	}

	s.logger.Info("scan complete", "pages", result.Count, "temp_dir", tempDir)
	return result, nil
}

func interleavePages(paths []string, tempDir string, logger *slog.Logger) []string {
	if len(paths) < 2 {
		return paths
	}

	n := len(paths)
	half := n / 2

	if n%2 != 0 {
		logger.Warn("odd number of pages in duplex scan, skipping reorder", "pages", n)
		return paths
	}

	reordered := make([]string, n)
	for i := 0; i < half; i++ {
		frontIdx := i
		backIdx := half + i

		newFrontIdx := i * 2
		newBackIdx := i*2 + 1

		oldFront := paths[frontIdx]
		oldBack := paths[backIdx]

		newFront := filepath.Join(tempDir, fmt.Sprintf("reordered_%03d.jpg", newFrontIdx+1))
		newBack := filepath.Join(tempDir, fmt.Sprintf("reordered_%03d.jpg", newBackIdx+1))

		if err := os.Rename(oldFront, newFront); err != nil {
			logger.Error("failed to rename front page", "error", err)
			reordered[newFrontIdx] = oldFront
		} else {
			reordered[newFrontIdx] = newFront
		}

		if err := os.Rename(oldBack, newBack); err != nil {
			logger.Error("failed to rename back page", "error", err)
			reordered[newBackIdx] = oldBack
		} else {
			reordered[newBackIdx] = newBack
		}
	}

	logger.Info("pages reordered", "pattern", "front1, back1, front2, back2, ...")
	return reordered
}

func (s *AirScanScanner) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.createClient(ctx)
	return err == nil
}
