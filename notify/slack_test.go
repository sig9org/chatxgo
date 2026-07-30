package notify

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlackText(t *testing.T) {
	msg := Message{
		Subject:  "Deploy",
		Body:     "**done**",
		Mentions: []Mention{{ID: "U0123456"}},
	}
	got := slackText(msg, []string{"- report.pdf (/tmp/report.pdf)"})
	want := "*Deploy*\n\n<@U0123456>\n\n**done**\n\n- report.pdf (/tmp/report.pdf)"
	if got != want {
		t.Errorf("slackText() =\n%q\nwant\n%q", got, want)
	}
}

func TestSlackSendRequiresDest(t *testing.T) {
	s := newSlackSender(SlackConfig{})
	if err := s.Send(context.Background(), Message{Body: "hi"}); err == nil {
		t.Error("expected error when SLACK_DST is missing")
	}
}

func TestSlackSendWebhookOnly(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newSlackSender(SlackConfig{Dest: srv.URL})
	if err := s.Send(context.Background(), Message{Body: "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["text"] != "hello" {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestSlackSendURLAttachmentWithoutToken(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newSlackSender(SlackConfig{Dest: srv.URL})
	err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{"https://example.com/a.png"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(got["text"], "https://example.com/a.png") {
		t.Errorf("expected attachment link in text, got %+v", got)
	}
}

func TestSlackSendLocalAttachmentWithoutTokenFallsBackToLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newSlackSender(SlackConfig{Dest: srv.URL})
	if err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{path}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(got["text"], "a.png") {
		t.Errorf("expected attachment filename in text, got %+v", got)
	}
}

func TestSlackSendLocalAttachmentUploadsWithToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("filedata"), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	var uploadedBytes []byte
	var completeBody map[string]any
	var uploadURL string

	mux.HandleFunc("/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer xoxb-test" {
			t.Errorf("unexpected auth header: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"upload_url": uploadURL,
			"file_id":    "F123",
		})
	})
	mux.HandleFunc("/upload-target", func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "file" {
					uploadedBytes, _ = io.ReadAll(part)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/files.completeUploadExternal", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&completeBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	uploadURL = srv.URL + "/upload-target"

	s := newSlackSender(SlackConfig{Dest: srv.URL + "/webhook", Token: "xoxb-test", Channel: "C1"})
	s.apiBase = srv.URL

	if err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{path}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(uploadedBytes) != "filedata" {
		t.Errorf("uploaded bytes = %q, want %q", uploadedBytes, "filedata")
	}
	files, ok := completeBody["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("unexpected completeUploadExternal body: %+v", completeBody)
	}
	first, _ := files[0].(map[string]any)
	if first["id"] != "F123" {
		t.Errorf("unexpected file id in completeUploadExternal: %+v", first)
	}
	if completeBody["channel_id"] != "C1" {
		t.Errorf("unexpected channel_id: %+v", completeBody)
	}
}

func TestSlackSendUploadFailsOnAPIError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("filedata"), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
	})
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := newSlackSender(SlackConfig{Dest: srv.URL + "/webhook", Token: "xoxb-test", Channel: "C1"})
	s.apiBase = srv.URL

	err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{path}})
	if err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("expected invalid_auth error, got %v", err)
	}
}
