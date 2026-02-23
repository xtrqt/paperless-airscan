package paperless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

type DocumentMeta struct {
	Title        string            `json:"title"`
	Created      time.Time         `json:"created"`
	Tags         []int             `json:"tags,omitempty"`
	CustomFields map[string]string `json:"custom_fields,omitempty"`
}

type Document struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Created     time.Time `json:"created"`
	ContentType string    `json:"content_type"`
	ArchiveFile string    `json:"archive_file"`
}

type UploadResponse struct {
	TaskID string `json:"task_id"`
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) doRequest(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Token "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.client.Do(req)
}

func (c *Client) UploadDocument(pdfPath string, meta DocumentMeta) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	file, err := os.Open(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("document", filepath.Base(pdfPath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	if meta.Title != "" {
		_ = writer.WriteField("title", meta.Title)
	}

	// Add custom fields if provided
	if len(meta.CustomFields) > 0 {
		for key, value := range meta.CustomFields {
			fieldName := fmt.Sprintf("custom_field_%s", key)
			_ = writer.WriteField(fieldName, value)
			slog.Info("adding custom field to upload", "field", fieldName, "value", value)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/documents/post_document/", &buf, writer.FormDataContentType())
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read the raw response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Log the raw response for debugging
	slog.Info("paperless upload response", "response", string(bodyBytes))

	// Try parsing as JSON object first ({"task_id": "..."})
	var result UploadResponse
	if err := json.Unmarshal(bodyBytes, &result); err == nil && result.TaskID != "" {
		return result.TaskID, nil
	}

	// Fallback: treat as plain string task ID (remove quotes if present)
	taskID := strings.Trim(string(bodyBytes), "\" \n\r\t")
	if taskID == "" {
		return "", fmt.Errorf("empty task ID in response: %s", string(bodyBytes))
	}

	return taskID, nil
}

func (c *Client) GetDocument(id int) (*Document, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/api/documents/%d/", id), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("document not found: %d", id)
	}

	var doc Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

func (c *Client) GetTaskStatus(taskID string) (string, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/api/tasks/%s/", taskID), nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("task not found: %s", taskID)
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Status, nil
}

func (c *Client) HealthCheck() error {
	resp, err := c.doRequest("GET", "/api/", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("paperless not healthy: status %d", resp.StatusCode)
	}

	return nil
}
