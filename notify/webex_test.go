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

func TestWebexMarkdown(t *testing.T) {
	msg := Message{
		Subject: "Deploy",
		Body:    "**done**",
		Mentions: []Mention{
			{ID: "alice@example.com", Label: "Alice"},
			{ID: "personid123"},
		},
	}
	got := webexMarkdown(msg)
	want := "**Deploy**\n\n<@personEmail:alice@example.com|Alice> <@personId:personid123|personid123>\n\n**done**"
	if got != want {
		t.Errorf("webexMarkdown() =\n%q\nwant\n%q", got, want)
	}
}

func TestWebexSendRequiresCredentials(t *testing.T) {
	s := newWebexSender(WebexConfig{})
	if err := s.Send(context.Background(), Message{Body: "hi"}); err == nil {
		t.Error("expected error when token/dest are missing")
	}
}

func TestWebexSendTextOnly(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newWebexSender(WebexConfig{Token: "tok", Dest: "room1"})
	s.apiBase = srv.URL

	err := s.Send(context.Background(), Message{Body: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody["roomId"] != "room1" || gotBody["markdown"] != "hello" {
		t.Errorf("unexpected request body: %+v", gotBody)
	}
}

func TestWebexSendWithURLAttachment(t *testing.T) {
	var gotBody map[string]any
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newWebexSender(WebexConfig{Token: "tok", Dest: "room1"})
	s.apiBase = srv.URL

	err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{"https://example.com/a.png"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected 1 request, got %d", requests)
	}
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "https://example.com/a.png" {
		t.Errorf("unexpected files field: %+v", gotBody["files"])
	}
}

func TestWebexSendWithLocalAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("filedata"), 0o600); err != nil {
		t.Fatal(err)
	}

	var requests []*http.Request
	var lastFileBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && strings.HasPrefix(mediaType, "multipart/") {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "files" {
					lastFileBytes, _ = io.ReadAll(part)
				}
			}
		}
		requests = append(requests, r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newWebexSender(WebexConfig{Token: "tok", Dest: "room1"})
	s.apiBase = srv.URL

	err := s.Send(context.Background(), Message{Body: "hello", Attachments: []string{path}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// One request for the text message, one for the file upload.
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	if string(lastFileBytes) != "filedata" {
		t.Errorf("uploaded file bytes = %q, want %q", lastFileBytes, "filedata")
	}
}
