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
	"strings"
	"unicode/utf8"
)

const discordContentLimit = 2000

type discordSender struct {
	cfg    DiscordConfig
	client *http.Client
}

func newDiscordSender(cfg DiscordConfig) *discordSender {
	return &discordSender{cfg: cfg, client: http.DefaultClient}
}

func (s *discordSender) Name() string { return "discord" }

type discordAllowedMentions struct {
	Parse []string `json:"parse"`
	Users []string `json:"users,omitempty"`
}

type discordPayload struct {
	Content         string                 `json:"content"`
	AllowedMentions discordAllowedMentions `json:"allowed_mentions"`
}

func discordText(msg Message, attachmentLines []string) string {
	var b strings.Builder
	if msg.Subject != "" {
		fmt.Fprintf(&b, "**%s**\n\n", msg.Subject)
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

func discordMessage(msg Message) (discordPayload, []localAttachment, error) {
	var linkLines []string
	var uploads []localAttachment
	for _, ref := range msg.Attachments {
		if isURL(ref) {
			linkLines = append(linkLines, formatAttachmentLine(ref))
			continue
		}
		attachment, err := readLocalAttachment(ref)
		if err != nil {
			return discordPayload{}, nil, err
		}
		uploads = append(uploads, attachment)
	}

	content := discordText(msg, linkLines)
	if utf8.RuneCountInString(content) > discordContentLimit {
		return discordPayload{}, nil, fmt.Errorf("discord: message content exceeds %d characters", discordContentLimit)
	}
	users := make([]string, len(msg.Mentions))
	for i, mention := range msg.Mentions {
		users[i] = mention.ID
	}
	return discordPayload{
		Content: content,
		AllowedMentions: discordAllowedMentions{
			Parse: []string{},
			Users: users,
		},
	}, uploads, nil
}

func (s *discordSender) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(s.cfg.Dest) == "" {
		return fmt.Errorf("discord: DISCORD_DST is required")
	}
	payload, uploads, err := discordMessage(msg)
	if err != nil {
		return err
	}

	var body bytes.Buffer
	contentType := "application/json"
	if len(uploads) == 0 {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return fmt.Errorf("discord: encode request: %w", err)
		}
	} else {
		writer := multipart.NewWriter(&body)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("discord: encode request: %w", err)
		}
		if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
			return fmt.Errorf("discord: build multipart: %w", err)
		}
		for i, attachment := range uploads {
			part, err := writer.CreateFormFile(fmt.Sprintf("files[%d]", i), attachment.Name)
			if err != nil {
				return fmt.Errorf("discord: build multipart: %w", err)
			}
			if _, err := part.Write(attachment.Data); err != nil {
				return fmt.Errorf("discord: build multipart: %w", err)
			}
		}
		if err := writer.Close(); err != nil {
			return fmt.Errorf("discord: build multipart: %w", err)
		}
		contentType = writer.FormDataContentType()
	}

	destination, err := url.Parse(s.cfg.Dest)
	if err != nil {
		return fmt.Errorf("discord: build request: %w", err)
	}
	query := destination.Query()
	query.Set("wait", "true")
	destination.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destination.String(), &body)
	if err != nil {
		return fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("discord: unexpected status %s: %s", resp.Status, string(responseBody))
	}
	return nil
}
