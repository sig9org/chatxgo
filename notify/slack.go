package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const slackAPIBase = "https://slack.com/api"

type slackSender struct {
	cfg     SlackConfig
	apiBase string
	client  *http.Client
}

func newSlackSender(cfg SlackConfig) *slackSender {
	return &slackSender{cfg: cfg, apiBase: slackAPIBase, client: http.DefaultClient}
}

func (s *slackSender) Name() string { return "slack" }

func slackText(msg Message, attachmentLines []string) string {
	var b strings.Builder
	if msg.Subject != "" {
		fmt.Fprintf(&b, "*%s*\n\n", msg.Subject)
	}
	if len(msg.Mentions) > 0 {
		mentions := make([]string, len(msg.Mentions))
		for i, m := range msg.Mentions {
			mentions[i] = fmt.Sprintf("<@%s>", m.ID)
		}
		b.WriteString(strings.Join(mentions, " "))
		b.WriteString("\n\n")
	}
	if msg.Body != "" {
		b.WriteString(msg.Body)
	}
	if len(attachmentLines) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(attachmentLines, "\n"))
	}
	return b.String()
}

// canUpload reports whether this sender is configured to upload local
// files via the Slack Web API, rather than only listing them as links.
func (s *slackSender) canUpload() bool {
	return s.cfg.Token != "" && s.cfg.Channel != ""
}

func (s *slackSender) Send(ctx context.Context, msg Message) error {
	if s.cfg.Dest == "" {
		return fmt.Errorf("slack: SLACK_DST is required")
	}

	var linkLines []string
	var uploads []string
	for _, a := range msg.Attachments {
		if !isURL(a) && s.canUpload() {
			uploads = append(uploads, a)
			continue
		}
		linkLines = append(linkLines, formatAttachmentLine(a))
	}

	if err := s.postWebhook(ctx, slackText(msg, linkLines)); err != nil {
		return err
	}
	for _, path := range uploads {
		if err := s.uploadFile(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *slackSender) postWebhook(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("slack: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Dest, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req, nil)
}

type slackAPIResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// uploadFile shares a local file to the configured channel using Slack's
// three-step external upload flow:
// https://api.slack.com/messaging/files#uploading_files
func (s *slackSender) uploadFile(ctx context.Context, path string) error {
	att, err := readLocalAttachment(path)
	if err != nil {
		return err
	}

	var start struct {
		slackAPIResponse
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	form := url.Values{
		"filename": {att.Name},
		"length":   {strconv.Itoa(len(att.Data))},
	}
	if err := s.callAPI(ctx, "files.getUploadURLExternal", form, &start); err != nil {
		return fmt.Errorf("slack: request upload url: %w", err)
	}
	if !start.OK {
		return fmt.Errorf("slack: files.getUploadURLExternal: %s", start.Error)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", att.Name)
	if err != nil {
		return fmt.Errorf("slack: build upload body: %w", err)
	}
	if _, err := part.Write(att.Data); err != nil {
		return fmt.Errorf("slack: build upload body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("slack: build upload body: %w", err)
	}
	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, start.UploadURL, &buf)
	if err != nil {
		return fmt.Errorf("slack: build upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", w.FormDataContentType())
	if err := s.do(uploadReq, nil); err != nil {
		return fmt.Errorf("slack: upload file: %w", err)
	}

	complete := map[string]any{
		"channel_id": s.cfg.Channel,
		"files":      []map[string]string{{"id": start.FileID, "title": att.Name}},
	}
	completeBody, err := json.Marshal(complete)
	if err != nil {
		return fmt.Errorf("slack: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase+"/files.completeUploadExternal", bytes.NewReader(completeBody))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	var result slackAPIResponse
	if err := s.do(req, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("slack: files.completeUploadExternal: %s", result.Error)
	}
	return nil
}

// callAPI POSTs form-encoded data to a Slack Web API method and decodes the
// JSON response into out.
func (s *slackSender) callAPI(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("slack: unexpected status %s: %s", resp.Status, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("slack: decode response: %w", err)
	}
	return nil
}

func (s *slackSender) do(req *http.Request, out any) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("slack: unexpected status %s: %s", resp.Status, string(b))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("slack: decode response: %w", err)
	}
	return nil
}
