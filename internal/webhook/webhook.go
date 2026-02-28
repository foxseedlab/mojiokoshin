package webhook

import "context"

type Sender interface {
	SendTranscript(ctx context.Context, filename string, body []byte) error
}
