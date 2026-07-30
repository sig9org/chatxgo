package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type teamsSender struct {
	cfg    TeamsConfig
	client *http.Client
}

func newTeamsSender(cfg TeamsConfig) *teamsSender {
	return &teamsSender{cfg: cfg, client: http.DefaultClient}
}

func (s *teamsSender) Name() string { return "teams" }

// teamsMentionEntity is the "msteams.entities" mention entry documented at
// https://learn.microsoft.com/microsoftteams/platform/task-modules-and-cards/cards/cards-format#mention-support-within-adaptive-cards
type teamsMentionEntity struct {
	Type      string            `json:"type"`
	Text      string            `json:"text"`
	Mentioned teamsMentionedRef `json:"mentioned"`
}

type teamsMentionedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// teamsTextBlock is an Adaptive Card TextBlock element. Its "text" supports
// a subset of Markdown (bold, italic, lists, links).
type teamsTextBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Weight string `json:"weight,omitempty"`
	Size   string `json:"size,omitempty"`
	Wrap   bool   `json:"wrap"`
}

// adaptiveCard is the Adaptive Card content Teams incoming webhooks expect,
// carried inside a "message" attachment.
type adaptiveCard struct {
	Schema  string           `json:"$schema"`
	Type    string           `json:"type"`
	Version string           `json:"version"`
	Body    []teamsTextBlock `json:"body"`
	MSTeams *struct {
		Entities []teamsMentionEntity `json:"entities"`
	} `json:"msteams,omitempty"`
}

type teamsAttachment struct {
	ContentType string       `json:"contentType"`
	Content     adaptiveCard `json:"content"`
}

// teamsMessage is the top-level payload posted to a Teams incoming webhook.
type teamsMessage struct {
	Type        string            `json:"type"`
	Attachments []teamsAttachment `json:"attachments"`
}

func teamsCard(msg Message) teamsMessage {
	var body []teamsTextBlock
	if msg.Subject != "" {
		body = append(body, teamsTextBlock{
			Type:   "TextBlock",
			Text:   msg.Subject,
			Weight: "Bolder",
			Size:   "Large",
			Wrap:   true,
		})
	}

	var text strings.Builder
	if len(msg.Mentions) > 0 {
		mentions := make([]string, len(msg.Mentions))
		for i, m := range msg.Mentions {
			mentions[i] = fmt.Sprintf("<at>%s</at>", m.label())
		}
		text.WriteString(strings.Join(mentions, " "))
	}
	if msg.Body != "" {
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(msg.Body)
	}
	if len(msg.Attachments) > 0 {
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		lines := make([]string, len(msg.Attachments))
		for i, a := range msg.Attachments {
			lines[i] = formatAttachmentLine(a)
		}
		text.WriteString(strings.Join(lines, "\n"))
	}
	if text.Len() > 0 {
		body = append(body, teamsTextBlock{Type: "TextBlock", Text: text.String(), Wrap: true})
	}

	card := adaptiveCard{
		Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
		Type:    "AdaptiveCard",
		Version: "1.4",
		Body:    body,
	}

	if len(msg.Mentions) > 0 {
		entities := make([]teamsMentionEntity, len(msg.Mentions))
		for i, m := range msg.Mentions {
			entities[i] = teamsMentionEntity{
				Type:      "mention",
				Text:      fmt.Sprintf("<at>%s</at>", m.label()),
				Mentioned: teamsMentionedRef{ID: m.ID, Name: m.label()},
			}
		}
		card.MSTeams = &struct {
			Entities []teamsMentionEntity `json:"entities"`
		}{Entities: entities}
	}

	return teamsMessage{
		Type: "message",
		Attachments: []teamsAttachment{
			{ContentType: "application/vnd.microsoft.card.adaptive", Content: card},
		},
	}
}

func (s *teamsSender) Send(ctx context.Context, msg Message) error {
	if s.cfg.Dest == "" {
		return fmt.Errorf("teams: MSTEAMS_DST is required")
	}

	body, err := json.Marshal(teamsCard(msg))
	if err != nil {
		return fmt.Errorf("teams: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Dest, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("teams: unexpected status %s: %s", resp.Status, string(b))
	}
	return nil
}
