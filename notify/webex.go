package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

const webexAPIBase = "https://webexapis.com/v1/messages"

type webexSender struct {
	cfg     WebexConfig
	apiBase string
	client  *http.Client
}

func newWebexSender(cfg WebexConfig) *webexSender {
	return &webexSender{cfg: cfg, apiBase: webexAPIBase, client: http.DefaultClient}
}

func (s *webexSender) Name() string { return "webex" }

func webexMention(m Mention) string {
	kind := "personId"
	if strings.Contains(m.ID, "@") {
		kind = "personEmail"
	}
	return fmt.Sprintf("<@%s:%s|%s>", kind, m.ID, m.label())
}

func webexMarkdown(msg Message) string {
	var b strings.Builder
	if msg.Subject != "" {
		fmt.Fprintf(&b, "**%s**\n\n", msg.Subject)
	}
	if len(msg.Mentions) > 0 {
		mentions := make([]string, len(msg.Mentions))
		for i, m := range msg.Mentions {
			mentions[i] = webexMention(m)
		}
		b.WriteString(strings.Join(mentions, " "))
		if msg.Body != "" {
			b.WriteString("\n\n")
		}
	}
	if msg.Body != "" {
		b.WriteString(msg.Body)
	}
	return b.String()
}

func (s *webexSender) Send(ctx context.Context, msg Message) error {
	if s.cfg.Token == "" || s.cfg.Dest == "" {
		return fmt.Errorf("webex: WEBEX_TOKEN and WEBEX_DST are required")
	}

	var urlAttachments, localAttachments []string
	for _, a := range msg.Attachments {
		if isURL(a) {
			urlAttachments = append(urlAttachments, a)
		} else {
			localAttachments = append(localAttachments, a)
		}
	}

	text := webexMarkdown(msg)
	if text != "" || len(urlAttachments) > 0 {
		if err := s.postJSON(ctx, text, urlAttachments); err != nil {
			return err
		}
	}
	for _, path := range localAttachments {
		if err := s.postFile(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *webexSender) postJSON(ctx context.Context, markdown string, fileURLs []string) error {
	payload := map[string]any{"roomId": s.cfg.Dest}
	if markdown != "" {
		payload["markdown"] = markdown
	}
	if len(fileURLs) > 0 {
		payload["files"] = fileURLs
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webex: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webex: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	return s.do(req)
}

func (s *webexSender) postFile(ctx context.Context, path string) error {
	att, err := readLocalAttachment(path)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("roomId", s.cfg.Dest); err != nil {
		return fmt.Errorf("webex: build multipart: %w", err)
	}
	part, err := w.CreateFormFile("files", att.Name)
	if err != nil {
		return fmt.Errorf("webex: build multipart: %w", err)
	}
	if _, err := part.Write(att.Data); err != nil {
		return fmt.Errorf("webex: build multipart: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("webex: build multipart: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase, &buf)
	if err != nil {
		return fmt.Errorf("webex: build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	return s.do(req)
}

func (s *webexSender) do(req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webex: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webex: unexpected status %s: %s", resp.Status, string(b))
	}
	return nil
}
