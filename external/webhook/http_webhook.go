package webhook

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"

	"github.com/foxseedlab/mojiokoshin/internal/webhook"
)

type HTTPSender struct {
	webhookURL string
	client     *http.Client
}

func NewHTTPSender(webhookURL string) webhook.Sender {
	return &HTTPSender{
		webhookURL: webhookURL,
		client:     &http.Client{},
	}
}

func (s *HTTPSender) SendTranscript(ctx context.Context, filename string, body []byte) error {
	if s.webhookURL == "" {
		return nil
	}

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	fileWriter, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := fileWriter.Write(body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, &payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if !isHTTPSuccessStatus(resp.StatusCode) {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func isHTTPSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}
