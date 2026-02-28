package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func (s *HTTPSender) SendTranscript(ctx context.Context, payload webhook.TranscriptWebhookPayload) error {
	if s.webhookURL == "" {
		return nil
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
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
