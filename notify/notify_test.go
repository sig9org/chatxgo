package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseMention(t *testing.T) {
	cases := []struct {
		in      string
		wantID  string
		wantLbl string
		wantErr bool
	}{
		{"U0123456", "U0123456", "", false},
		{"user@example.com:Alice", "user@example.com", "Alice", false},
		{" id : label ", "id", "label", false},
		{"", "", "", true},
		{":no-id", "", "", true},
	}
	for _, c := range cases {
		got, err := ParseMention(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseMention(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseMention(%q): unexpected error: %v", c.in, err)
		}
		if got.ID != c.wantID || got.Label != c.wantLbl {
			t.Errorf("ParseMention(%q) = %+v, want ID=%q Label=%q", c.in, got, c.wantID, c.wantLbl)
		}
	}
}

func TestMentionLabel(t *testing.T) {
	if got := (Mention{ID: "id1"}).label(); got != "id1" {
		t.Errorf("label() = %q, want %q", got, "id1")
	}
	if got := (Mention{ID: "id1", Label: "Alice"}).label(); got != "Alice" {
		t.Errorf("label() = %q, want %q", got, "Alice")
	}
}

func TestMessageValidate(t *testing.T) {
	if err := (Message{}).Validate(); err == nil {
		t.Error("empty message should fail validation")
	}
	if err := (Message{Subject: "hi"}).Validate(); err != nil {
		t.Errorf("subject-only message should validate: %v", err)
	}
	if err := (Message{Body: "hi"}).Validate(); err != nil {
		t.Errorf("body-only message should validate: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	for _, kv := range [][2]string{
		{"WEBEX_TOKEN", "tok"}, {"WEBEX_DST", "room1"},
		{"MSTEAMS_DST", ""},
		{"SLACK_DST", "https://hooks.slack/x"},
		{"SLACK_TOKEN", "xoxb"}, {"SLACK_CHANNEL", "C1"},
		{"PROXY", "http://proxy.example:8080"},
	} {
		t.Setenv(kv[0], kv[1])
	}

	cfg := ConfigFromEnv()
	if cfg.Webex.Token != "tok" || cfg.Webex.Dest != "room1" {
		t.Errorf("unexpected webex config: %+v", cfg.Webex)
	}
	if cfg.Teams.Dest != "" {
		t.Errorf("teams should have no destination: %+v", cfg.Teams)
	}
	if cfg.Slack.Dest == "" || cfg.Slack.Token != "xoxb" || cfg.Slack.Channel != "C1" {
		t.Errorf("unexpected slack config: %+v", cfg.Slack)
	}
	if cfg.Proxy != "http://proxy.example:8080" {
		t.Errorf("unexpected proxy: %q", cfg.Proxy)
	}
}

func TestSendersOrderAndFiltering(t *testing.T) {
	cfg := Config{
		Webex: WebexConfig{Token: "t", Dest: "d"},
		Slack: SlackConfig{Dest: "d"},
	}
	senders, err := Senders(cfg)
	if err != nil {
		t.Fatalf("Senders: %v", err)
	}
	if len(senders) != 2 {
		t.Fatalf("expected 2 senders, got %d", len(senders))
	}
	if senders[0].Name() != "webex" || senders[1].Name() != "slack" {
		t.Errorf("unexpected sender order: %s, %s", senders[0].Name(), senders[1].Name())
	}
}

func TestSendersDisabledWhenDestBlank(t *testing.T) {
	cfg := Config{
		Webex: WebexConfig{Token: "t", Dest: "  "},
		Teams: TeamsConfig{Dest: ""},
		Slack: SlackConfig{Dest: "d"},
	}
	senders, err := Senders(cfg)
	if err != nil {
		t.Fatalf("Senders: %v", err)
	}
	if len(senders) != 1 || senders[0].Name() != "slack" {
		t.Errorf("expected only slack enabled, got %v", senders)
	}
}

func TestSendersInvalidProxyURL(t *testing.T) {
	cfg := Config{Proxy: "://bad", Slack: SlackConfig{Dest: "https://example.com"}}
	if _, err := Senders(cfg); err == nil {
		t.Error("expected an error for an invalid proxy URL")
	}
}

func TestSendersRouteThroughProxy(t *testing.T) {
	var destHits, proxyHits int
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer dest.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	cfg := Config{Proxy: proxy.URL, Slack: SlackConfig{Dest: dest.URL}}
	senders, err := Senders(cfg)
	if err != nil {
		t.Fatalf("Senders: %v", err)
	}
	if len(senders) != 1 {
		t.Fatalf("expected 1 sender, got %d", len(senders))
	}

	if err := senders[0].Send(context.Background(), Message{Body: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if proxyHits != 1 {
		t.Errorf("proxy hits = %d, want 1", proxyHits)
	}
	if destHits != 0 {
		t.Errorf("destination hits = %d, want 0 (request should have gone through the proxy)", destHits)
	}
}

func TestNewDispatcherInvalidProxyURL(t *testing.T) {
	cfg := Config{Proxy: "://bad", Slack: SlackConfig{Dest: "https://example.com"}}
	if _, err := NewDispatcher(cfg); err == nil {
		t.Error("expected an error for an invalid proxy URL")
	}
}

type stubSender struct {
	name string
	err  error
	got  Message
}

func (s *stubSender) Name() string { return s.name }
func (s *stubSender) Send(_ context.Context, msg Message) error {
	s.got = msg
	return s.err
}

func TestDispatcherSendNoRecipients(t *testing.T) {
	d := &Dispatcher{}
	_, err := d.Send(context.Background(), Message{Body: "hi"})
	if !errors.Is(err, ErrNoRecipients) {
		t.Errorf("expected ErrNoRecipients, got %v", err)
	}
}

func TestDispatcherSendValidatesMessage(t *testing.T) {
	d := &Dispatcher{senders: []Sender{&stubSender{name: "x"}}}
	_, err := d.Send(context.Background(), Message{})
	if err == nil {
		t.Error("expected validation error for empty message")
	}
}

func TestDispatcherSendAggregatesResults(t *testing.T) {
	ok := &stubSender{name: "ok"}
	fail := &stubSender{name: "fail", err: errors.New("boom")}
	d := &Dispatcher{senders: []Sender{ok, fail}}

	results, err := d.Send(context.Background(), Message{Body: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Tool != "ok" || results[0].Err != nil {
		t.Errorf("unexpected result[0]: %+v", results[0])
	}
	if results[1].Tool != "fail" || results[1].Err == nil {
		t.Errorf("unexpected result[1]: %+v", results[1])
	}
	if ok.got.Body != "hello" {
		t.Errorf("sender did not receive message body: %+v", ok.got)
	}
}
