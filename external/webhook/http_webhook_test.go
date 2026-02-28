package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendTranscript_EmptyWebhookURL(t *testing.T) {
	sender := NewHTTPSender("")
	if err := sender.SendTranscript(context.Background(), "a.txt", []byte("hello")); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSendTranscript_Success(t *testing.T) {
	var gotFilename string
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		mediaType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(mediaType, "multipart/form-data") {
			t.Fatalf("unexpected content type: %s", mediaType)
		}

		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("failed to create multipart reader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("failed to read multipart part: %v", err)
		}
		if part.FormName() != "file" {
			t.Fatalf("unexpected form name: %s", part.FormName())
		}
		gotFilename = part.FileName()
		content, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("failed to read file body: %v", err)
		}
		gotBody = string(content)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHTTPSender(server.URL)
	if err := sender.SendTranscript(context.Background(), "transcript.txt", []byte("hello world")); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if gotFilename != "transcript.txt" {
		t.Fatalf("unexpected filename: %s", gotFilename)
	}
	if gotBody != "hello world" {
		t.Fatalf("unexpected body: %s", gotBody)
	}
}

func TestSendTranscript_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	sender := NewHTTPSender(server.URL)
	if err := sender.SendTranscript(context.Background(), "transcript.txt", []byte("hello")); err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}
