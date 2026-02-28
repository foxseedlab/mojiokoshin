package transcriber

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/auth/credentials"
	speech "cloud.google.com/go/speech/apiv2"
	speechpb "cloud.google.com/go/speech/apiv2/speechpb"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	speechAPIEndpointPort = 443
	audioSampleRateHertz  = 48000
	audioChannelCount     = 2
)

type CloudSpeechConfig struct {
	ProjectID       string
	CredentialsJSON string
	Language        string
	Location        string
	Model           string
}

type CloudSpeechTranscriber struct {
	projectID       string
	credentialsJSON string
	defaultLanguage string
	location        string
	model           string
}

func NewCloudSpeechTranscriber(cfg CloudSpeechConfig) transcriber.Transcriber {
	location := strings.TrimSpace(cfg.Location)
	model := strings.TrimSpace(cfg.Model)

	return &CloudSpeechTranscriber{
		projectID:       cfg.ProjectID,
		credentialsJSON: cfg.CredentialsJSON,
		defaultLanguage: cfg.Language,
		location:        location,
		model:           model,
	}
}

func (t *CloudSpeechTranscriber) StartStreaming(ctx context.Context, sessionID, language string, receiver transcriber.ResultReceiver) (transcriber.StreamWriter, error) {
	slog.Info("starting cloud speech streaming", "session_id", sessionID, "location", t.location, "language", language, "model", t.model)
	if language == "" {
		language = t.defaultLanguage
	}

	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		CredentialsJSON: []byte(t.credentialsJSON),
		Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
	})
	if err != nil {
		return nil, fmt.Errorf("detect credentials: %w", err)
	}

	opts := []option.ClientOption{
		option.WithAuthCredentials(creds),
	}
	if t.location != "global" {
		opts = append(opts, option.WithEndpoint(fmt.Sprintf("%s-speech.googleapis.com:%d", t.location, speechAPIEndpointPort)))
	}

	client, err := speech.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	stream, err := client.StreamingRecognize(ctx)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	recognizer := fmt.Sprintf("projects/%s/locations/%s/recognizers/_", t.projectID, t.location)
	sendConfig := func(s speechpb.Speech_StreamingRecognizeClient) error {
		return s.Send(&speechpb.StreamingRecognizeRequest{
			Recognizer: recognizer,
			StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
				StreamingConfig: &speechpb.StreamingRecognitionConfig{
					Config: &speechpb.RecognitionConfig{
						Model:         t.model,
						LanguageCodes: []string{language},
						DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
							ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
								Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
								SampleRateHertz:   audioSampleRateHertz,
								AudioChannelCount: audioChannelCount,
							},
						},
						Features: &speechpb.RecognitionFeatures{},
					},
					StreamingFeatures: &speechpb.StreamingRecognitionFeatures{InterimResults: true},
				},
			},
		})
	}
	if err := sendConfig(stream); err != nil {
		_ = stream.CloseSend()
		_ = client.Close()
		return nil, err
	}
	slog.Info("cloud speech stream initialized", "session_id", sessionID)

	w := &streamWriter{
		stream:   stream,
		receiver: receiver,
		newStreamFn: func() (speechpb.Speech_StreamingRecognizeClient, error) {
			next, err := client.StreamingRecognize(ctx)
			if err != nil {
				return nil, err
			}
			if err := sendConfig(next); err != nil {
				_ = next.CloseSend()
				return nil, err
			}
			return next, nil
		},
		closeFn: func() error {
			return client.Close()
		},
	}
	w.startReceiver(stream, receiver)

	return w, nil
}

type streamWriter struct {
	mu          sync.Mutex
	closed      bool
	stream      speechpb.Speech_StreamingRecognizeClient
	receiver    transcriber.ResultReceiver
	newStreamFn func() (speechpb.Speech_StreamingRecognizeClient, error)
	closeFn     func() error
}

func (w *streamWriter) Write(pcm []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return io.ErrClosedPipe
	}
	req := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{
			Audio: pcm,
		},
	}
	if err := w.stream.Send(req); err != nil {
		if !isReconnectableStreamError(err) {
			return err
		}
		slog.Warn("transcriber send failed with reconnectable error; reconnecting", "error", err)
		if err := w.reconnectLocked(); err != nil {
			return fmt.Errorf("reconnect stream: %w", err)
		}
		return w.stream.Send(req)
	}
	return nil
}

func (w *streamWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.stream.CloseSend(); err != nil {
		_ = w.closeFn()
		return err
	}
	return w.closeFn()
}

func (w *streamWriter) reconnectLocked() error {
	slog.Warn("transcriber stream aborted; reconnecting")
	_ = w.stream.CloseSend()
	next, err := w.newStreamFn()
	if err != nil {
		slog.Error("failed to reconnect transcriber stream", "error", err)
		return err
	}
	w.stream = next
	w.startReceiver(next, w.receiver)
	slog.Info("transcriber stream reconnected")
	return nil
}

func (w *streamWriter) startReceiver(stream speechpb.Speech_StreamingRecognizeClient, receiver transcriber.ResultReceiver) {
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF || strings.Contains(err.Error(), "context canceled") {
					slog.Info("transcriber receive loop stopped", "reason", err.Error())
					return
				}
				if isReconnectableStreamError(err) {
					slog.Warn("transcriber receive loop ended with reconnectable abort", "error", err)
					return
				}
				receiver.OnError(err)
				return
			}
			for i, result := range resp.GetResults() {
				if len(result.GetAlternatives()) == 0 {
					continue
				}
				receiver.OnResult(i, result.GetAlternatives()[0].GetTranscript(), result.GetIsFinal())
			}
		}
	}()
}

func isReconnectableStreamError(err error) bool {
	if err == io.EOF || strings.Contains(strings.ToLower(err.Error()), "eof") {
		return true
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Aborted {
		return false
	}
	msg := strings.ToLower(st.Message())
	return strings.Contains(msg, "max duration of 5 minutes") ||
		strings.Contains(msg, "stream timed out after receiving no more client requests")
}
