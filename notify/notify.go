// Package notify sends Markdown-formatted messages to chat tools (Cisco
// Webex, Microsoft Teams, Slack). It can be used as a library, or driven by
// the chatxgo CLI.
package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sig9org/chatxgo/internal/debugx"
)

// ErrNoRecipients is returned when no chat tool is enabled in the config.
var ErrNoRecipients = errors.New("notify: no chat tool is enabled")

// Mention identifies a single user to mention in a message. ID is the
// native identifier expected by the target tool (a Slack user ID such as
// "U0123456", a Webex person email address, or for Teams a Microsoft
// Entra object ID or user principal name/email). Label is the display
// name shown in the message text; if empty, ID is shown instead.
type Mention struct {
	ID    string
	Label string
}

func (m Mention) label() string {
	if m.Label != "" {
		return m.Label
	}
	return m.ID
}

// ParseMention parses a single "-mention" value in the form "id" or
// "id:label".
func ParseMention(raw string) (Mention, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Mention{}, errors.New("notify: empty mention")
	}
	id, label, _ := strings.Cut(raw, ":")
	id = strings.TrimSpace(id)
	if id == "" {
		return Mention{}, fmt.Errorf("notify: invalid mention %q", raw)
	}
	return Mention{ID: id, Label: strings.TrimSpace(label)}, nil
}

// Message is a single notification to deliver to one or more chat tools.
type Message struct {
	// Subject is an optional title/summary shown as a heading (Webex/Slack)
	// or card title (Teams).
	Subject string
	// Body is the message content, formatted as Markdown.
	Body string
	// Mentions lists users to call out in the message.
	Mentions []Mention
	// Attachments lists local file paths or URLs to attach/link.
	Attachments []string
}

// Validate checks that the message has enough content to send.
func (m Message) Validate() error {
	if strings.TrimSpace(m.Subject) == "" && strings.TrimSpace(m.Body) == "" {
		return errors.New("notify: message has neither subject nor body")
	}
	return nil
}

// Sender delivers a Message to a single chat tool.
type Sender interface {
	// Name identifies the chat tool, e.g. "webex", "teams", "slack".
	Name() string
	// Send delivers msg. Implementations should respect ctx cancellation.
	Send(ctx context.Context, msg Message) error
}

// WebexConfig configures the Webex sender. It is enabled when Dest is set.
type WebexConfig struct {
	// Token is a Webex bot/personal access token.
	Token string
	// Dest is the destination room ID (roomId).
	Dest string
}

// TeamsConfig configures the Microsoft Teams sender. It is enabled when
// Dest is set.
type TeamsConfig struct {
	// Dest is the Teams incoming webhook URL.
	Dest string
}

// SlackConfig configures the Slack sender. It is enabled when Dest is set.
type SlackConfig struct {
	// Dest is the Slack incoming webhook URL, used for the message text.
	Dest string
	// Token is an optional Slack bot token (xoxb-...), required only to
	// upload local file attachments via the Web API.
	Token string
	// Channel is the channel ID/name used when uploading attachments.
	// Required only when Token is set and attachments are sent.
	Channel string
}

// Config aggregates the settings for every supported chat tool. A tool is
// enabled simply by giving it a destination (Dest); a zero-value Dest means
// the tool is disabled and its other fields are ignored.
type Config struct {
	Webex WebexConfig
	Teams TeamsConfig
	Slack SlackConfig
}

// ConfigFromEnv builds a Config by reading environment variables. Prefer
// LoadConfigFile for config.ini-based settings; this is for callers that
// keep settings in the process environment instead.
//
// Recognized variables:
//
//	WEBEX_TOKEN, WEBEX_DST
//	MSTEAMS_DST
//	SLACK_DST, SLACK_TOKEN, SLACK_CHANNEL
func ConfigFromEnv() Config {
	return Config{
		Webex: WebexConfig{
			Token: os.Getenv("WEBEX_TOKEN"),
			Dest:  os.Getenv("WEBEX_DST"),
		},
		Teams: TeamsConfig{
			Dest: os.Getenv("MSTEAMS_DST"),
		},
		Slack: SlackConfig{
			Dest:    os.Getenv("SLACK_DST"),
			Token:   os.Getenv("SLACK_TOKEN"),
			Channel: os.Getenv("SLACK_CHANNEL"),
		},
	}
}

// Senders returns a Sender for every chat tool in cfg that has a
// destination configured, in a deterministic order (Webex, Teams, Slack).
func Senders(cfg Config) []Sender {
	var out []Sender
	if strings.TrimSpace(cfg.Webex.Dest) != "" {
		out = append(out, newWebexSender(cfg.Webex))
	}
	if strings.TrimSpace(cfg.Teams.Dest) != "" {
		out = append(out, newTeamsSender(cfg.Teams))
	}
	if strings.TrimSpace(cfg.Slack.Dest) != "" {
		out = append(out, newSlackSender(cfg.Slack))
	}
	return out
}

// Result is the outcome of sending a Message through a single Sender.
type Result struct {
	Tool string
	Err  error
}

// Dispatcher sends messages to every enabled chat tool.
type Dispatcher struct {
	senders []Sender
}

// NewDispatcher builds a Dispatcher from cfg. It holds no enabled senders
// (and Send returns ErrNoRecipients) if every tool is disabled.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{senders: Senders(cfg)}
}

// Send delivers msg to every enabled chat tool and returns one Result per
// tool. It returns ErrNoRecipients (with a nil Results slice) if no tool is
// enabled.
func (d *Dispatcher) Send(ctx context.Context, msg Message) ([]Result, error) {
	if len(d.senders) == 0 {
		return nil, ErrNoRecipients
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(d.senders))
	for _, s := range d.senders {
		debugx.Printf("sending message to %s", s.Name())
		err := s.Send(ctx, msg)
		if err != nil {
			debugx.Printf("%s: send failed: %v", s.Name(), err)
		} else {
			debugx.Printf("%s: sent", s.Name())
		}
		results = append(results, Result{Tool: s.Name(), Err: err})
	}
	return results, nil
}
