package pdf

import (
	"fmt"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// InjectFilingMetadata adds custom fields to PDF Info dictionary
func InjectFilingMetadata(pdfPath string, filingID string, pageCount int, scanDate time.Time) error {
	// Create properties map for PDF Info dictionary
	properties := map[string]string{
		"FilingID":  filingID,
		"PageCount": fmt.Sprintf("%d", pageCount),
		"ScanDate":  scanDate.Format(time.RFC3339),
		"Subject":   fmt.Sprintf("Filing ID: %s", filingID),
	}

	// Add properties to PDF
	if err := api.AddPropertiesFile(pdfPath, "", properties, nil); err != nil {
		return fmt.Errorf("add properties to PDF: %w", err)
	}

	return nil
}
