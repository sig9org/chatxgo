package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscordSendRequiresDest(t *testing.T) {
	sender := newDiscordSender(DiscordConfig{})
	if err := sender.Send(context.Background(), Message{Body: "hello"}); err == nil {
		t.Error("expected error when DISCORD_DST is missing")
	}
}

func TestDiscordSendJSON(t *testing.T) {
	var got discordPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" {
			t.Errorf("wait = %q, want true", r.URL.Query().Get("wait"))
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := newDiscordSender(DiscordConfig{Dest: server.URL})
	msg := Message{
		Subject:  "Deploy",
		Body:     "hello",
		Mentions: []Mention{{ID: "123", Label: "Alice"}},
		Attachments: []string{
			"https://example.com/report.pdf",
		},
	}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	wantContent := "**Deploy**\n\n<@123>\n\nhello\n\n- [report.pdf](https://example.com/report.pdf)"
	if got.Content != wantContent {
		t.Errorf("content = %q, want %q", got.Content, wantContent)
	}
	if got.AllowedMentions.Parse == nil || len(got.AllowedMentions.Parse) != 0 {
		t.Errorf("allowed mention parse types = %#v, want empty", got.AllowedMentions.Parse)
	}
	if len(got.AllowedMentions.Users) != 1 || got.AllowedMentions.Users[0] != "123" {
		t.Errorf("allowed mention users = %#v, want [123]", got.AllowedMentions.Users)
	}
}

func TestDiscordSendLocalAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(path, []byte("report data"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload discordPayload
		if err := json.Unmarshal([]byte(r.FormValue("payload_json")), &payload); err != nil {
			t.Errorf("decode payload_json: %v", err)
		}
		if payload.Content != "hello" {
			t.Errorf("content = %q, want hello", payload.Content)
		}
		file, header, err := r.FormFile("files[0]")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("read file: %v", err)
		}
		if header.Filename != "report.txt" || string(data) != "report data" {
			t.Errorf("attachment = %q %q", header.Filename, data)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := newDiscordSender(DiscordConfig{Dest: server.URL})
	if err := sender.Send(context.Background(), Message{Body: "hello", Attachments: []string{path}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestDiscordSendRejectsLongContent(t *testing.T) {
	sender := newDiscordSender(DiscordConfig{Dest: "https://example.com/webhook"})
	err := sender.Send(context.Background(), Message{Body: strings.Repeat("x", discordContentLimit+1)})
	if err == nil || !strings.Contains(err.Error(), "exceeds 2000 characters") {
		t.Errorf("error = %v, want content-limit error", err)
	}
}

func TestDiscordSendErrorOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad webhook", http.StatusBadRequest)
	}))
	defer server.Close()

	sender := newDiscordSender(DiscordConfig{Dest: server.URL})
	err := sender.Send(context.Background(), Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Errorf("error = %v, want bad status", err)
	}
}
