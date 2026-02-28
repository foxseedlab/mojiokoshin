package transcriber

import "context"

type StreamWriter interface {
	Write(pcm []byte) error
	Close() error
}

type ResultReceiver interface {
	OnResult(segmentIndex int, text string, isFinal bool)
	OnError(err error)
}

type Transcriber interface {
	StartStreaming(ctx context.Context, sessionID, language string, receiver ResultReceiver) (StreamWriter, error)
}
