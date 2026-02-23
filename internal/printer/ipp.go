package printer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	goipp "github.com/OpenPrinting/goipp"
)

type IPPClient struct {
	logger *slog.Logger
}

func NewIPPClient(logger *slog.Logger) *IPPClient {
	return &IPPClient{
		logger: logger,
	}
}

// PrintPDF sends a PDF to the printer via IPP protocol
func (c *IPPClient) PrintPDF(ctx context.Context, printerHost string, pdfData []byte, jobName string) error {
	c.logger.Info("sending print job", "printer", printerHost, "job_name", jobName, "size_bytes", len(pdfData))

	printerURL := fmt.Sprintf("ipp://%s:631/ipp/print", printerHost)
	httpURL := fmt.Sprintf("http://%s:631/ipp/print", printerHost)

	// Build IPP request
	req := goipp.NewRequest(goipp.DefaultVersion, goipp.OpPrintJob, 1)
	req.Operation.Add(goipp.MakeAttribute("attributes-charset", goipp.TagCharset, goipp.String("utf-8")))
	req.Operation.Add(goipp.MakeAttribute("attributes-natural-language", goipp.TagLanguage, goipp.String("en-US")))
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(printerURL)))
	req.Operation.Add(goipp.MakeAttribute("requesting-user-name", goipp.TagName, goipp.String("paperless-airscan")))
	req.Operation.Add(goipp.MakeAttribute("job-name", goipp.TagName, goipp.String(jobName)))
	req.Operation.Add(goipp.MakeAttribute("document-format", goipp.TagMimeType, goipp.String("application/pdf")))

	// Job template attributes
	req.Job.Add(goipp.MakeAttribute("media-source", goipp.TagKeyword, goipp.String("auto")))
	// req.Job.Add(goipp.MakeAttribute("media-type", goipp.TagKeyword, goipp.String("auto")))

	// Encode IPP request
	payload, err := req.EncodeBytes()
	if err != nil {
		c.logger.Error("failed to encode IPP request", "error", err)
		return fmt.Errorf("encode IPP request: %w", err)
	}

	// Build HTTP request with IPP payload + PDF data
	body := io.MultiReader(bytes.NewBuffer(payload), bytes.NewReader(pdfData))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL, body)
	if err != nil {
		c.logger.Error("failed to create HTTP request", "error", err)
		return fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("content-type", goipp.ContentType)
	httpReq.Header.Set("accept", goipp.ContentType)

	// Execute HTTP request
	httpResp, err := http.DefaultClient.Do(httpReq)
	if httpResp != nil {
		defer httpResp.Body.Close()
	}
	if err != nil {
		c.logger.Error("HTTP request failed", "error", err, "url", httpURL)
		return fmt.Errorf("HTTP request: %w", err)
	}

	if httpResp.StatusCode/100 != 2 {
		c.logger.Error("HTTP error", "status", httpResp.Status, "url", httpURL)
		return fmt.Errorf("HTTP error: %s", httpResp.Status)
	}

	// Decode IPP response
	resp := &goipp.Message{}
	if err := resp.Decode(httpResp.Body); err != nil {
		c.logger.Error("failed to decode IPP response", "error", err)
		return fmt.Errorf("decode IPP response: %w", err)
	}

	if goipp.Status(resp.Code) != goipp.StatusOk {
		c.logger.Error("IPP error", "status", goipp.Status(resp.Code).String(), "code", resp.Code)
		return fmt.Errorf("IPP error: %s", goipp.Status(resp.Code).String())
	}

	c.logger.Info("print job submitted successfully", "printer", printerHost)
	return nil
}
