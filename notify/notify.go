// Package notify sends Markdown-formatted messages to chat tools (Cisco
// Webex, Microsoft Teams, Slack, Discord) and email. It can be used as a library, or
// driven by the chatxgo CLI.
package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sig9org/chatxgo/internal/debugx"
)

func envList(name string) []string {
	var out []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func envInt(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// ErrNoRecipients is returned when no chat tool is enabled in the config.
var ErrNoRecipients = errors.New("notify: no chat tool is enabled")

// Mention identifies a single user to mention in a message. ID is the
// native identifier expected by the target tool (a Slack or Discord user ID,
// a Webex person email address, or for Teams a Microsoft Entra object ID or
// user principal name/email). Label is the display name shown in the message
// text where supported; if empty, ID is shown instead.
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
	// Subject is an optional title/summary shown as a heading
	// (Webex/Slack/Discord) or card title (Teams).
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
	// Name identifies the chat tool, e.g. "webex", "teams", "slack",
	// or "discord".
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

// DiscordConfig configures the Discord sender. It is enabled when Dest is set.
type DiscordConfig struct {
	// Dest is the Discord incoming webhook URL.
	Dest string
}

// EmailConfig configures SMTP email delivery. It is enabled when Host, From,
// and at least one recipient are set. Port 465 uses implicit TLS; port 587
// requires STARTTLS; other ports (including the traditional 25) use STARTTLS
// when the server advertises it.
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	Cc       []string
	Bcc      []string
}

// Config aggregates the settings for every supported chat tool. A tool is
// enabled simply by giving it a destination (Dest); a zero-value Dest means
// the tool is disabled and its other fields are ignored.
type Config struct {
	Webex   WebexConfig
	Teams   TeamsConfig
	Slack   SlackConfig
	Discord DiscordConfig
	Email   EmailConfig
	// Proxy is an optional HTTP(S) proxy URL (e.g.
	// "http://user:pass@proxy.example:8080") that every enabled tool's
	// requests are routed through. A blank Proxy sends requests directly.
	Proxy string
}

// ConfigFromEnv builds a Config by reading environment variables. Prefer
// LoadConfigFile for config.toml-based settings; this is for callers that
// keep settings in the process environment instead.
//
// Recognized variables:
//
//	WEBEX_TOKEN, WEBEX_DST
//	MSTEAMS_DST
//	SLACK_DST, SLACK_TOKEN, SLACK_CHANNEL
//	DISCORD_DST
//	PROXY
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
		Discord: DiscordConfig{
			Dest: os.Getenv("DISCORD_DST"),
		},
		Email: EmailConfig{
			Host: os.Getenv("EMAIL_SMTP_HOST"), Port: envInt("EMAIL_SMTP_PORT", 25),
			Username: os.Getenv("EMAIL_SMTP_USERNAME"), Password: os.Getenv("EMAIL_SMTP_PASSWORD"),
			From: os.Getenv("EMAIL_FROM"), To: envList("EMAIL_TO"), Cc: envList("EMAIL_CC"), Bcc: envList("EMAIL_BCC"),
		},
		Proxy: os.Getenv("PROXY"),
	}
}

// Senders returns a Sender for every chat tool in cfg that has a
// destination configured, in a deterministic order (Webex, Teams, Slack,
// Discord).
// Every returned Sender routes its requests through cfg.Proxy, if set. It
// returns an error if cfg.Proxy is set but not a valid proxy URL.
func Senders(cfg Config) ([]Sender, error) {
	client, err := proxyHTTPClient(cfg.Proxy)
	if err != nil {
		return nil, err
	}
	var out []Sender
	if strings.TrimSpace(cfg.Webex.Dest) != "" {
		s := newWebexSender(cfg.Webex)
		s.client = client
		out = append(out, s)
	}
	if strings.TrimSpace(cfg.Teams.Dest) != "" {
		s := newTeamsSender(cfg.Teams)
		s.client = client
		out = append(out, s)
	}
	if strings.TrimSpace(cfg.Slack.Dest) != "" {
		s := newSlackSender(cfg.Slack)
		s.client = client
		out = append(out, s)
	}
	if strings.TrimSpace(cfg.Discord.Dest) != "" {
		s := newDiscordSender(cfg.Discord)
		s.client = client
		out = append(out, s)
	}
	if emailEnabled(cfg.Email) {
		out = append(out, newEmailSender(cfg.Email))
	}
	return out, nil
}

// Result is the outcome of sending a Message through a single Sender.
type Result struct {
	Tool string
	Err  error
}

// ToolDisplayName returns the human-readable product name for a Sender name.
// Unknown names are returned unchanged so custom Sender implementations remain
// useful in diagnostics.
func ToolDisplayName(tool string) string {
	switch tool {
	case "webex":
		return "Cisco Webex"
	case "discord":
		return "Discord"
	case "teams":
		return "Microsoft Teams"
	case "slack":
		return "Slack"
	case "email":
		return "Email"
	default:
		return tool
	}
}

// Dispatcher sends messages to every enabled chat tool.
type Dispatcher struct {
	senders []Sender
}

// NewDispatcher builds a Dispatcher from cfg. It holds no enabled senders
// (and Send returns ErrNoRecipients) if every tool is disabled. It returns
// an error if cfg.Proxy is set but not a valid proxy URL.
func NewDispatcher(cfg Config) (*Dispatcher, error) {
	senders, err := Senders(cfg)
	if err != nil {
		return nil, err
	}
	return &Dispatcher{senders: senders}, nil
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
		displayName := ToolDisplayName(s.Name())
		debugx.Printf("sending message to %s", displayName)
		err := s.Send(ctx, msg)
		if err != nil {
			debugx.Printf("%s: send failed: %v", displayName, err)
		} else {
			debugx.Printf("%s: sent", displayName)
		}
		results = append(results, Result{Tool: s.Name(), Err: err})
	}
	return results, nil
}
