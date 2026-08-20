package notify

import (
	"strings"
	"testing"
)

func TestEmailMessageRecipientsAndHeaders(t *testing.T) {
	body, recipients, err := emailMessage(EmailConfig{
		From: "Sender <sender@example.com>",
		To:   []string{"to1@example.com", "to2@example.com"},
		Cc:   []string{"cc@example.com"}, Bcc: []string{"hidden@example.com"},
	}, Message{Subject: "日本語 subject", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "hidden@example.com") {
		t.Error("Bcc address must not be included in message headers")
	}
	for _, want := range []string{"To: to1@example.com, to2@example.com\r\n", "Cc: cc@example.com\r\n", "hello\r\n"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("message missing %q: %q", want, body)
		}
	}
	if got, want := strings.Join(recipients, ","), "to1@example.com,to2@example.com,cc@example.com,hidden@example.com"; got != want {
		t.Errorf("envelope recipients = %q, want %q", got, want)
	}
}

func TestEmailEnabledAndSender(t *testing.T) {
	cfg := Config{Email: EmailConfig{Host: "smtp.example.com", From: "from@example.com", To: []string{"to@example.com"}}}
	senders, err := Senders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(senders) != 1 || senders[0].Name() != "email" {
		t.Fatalf("senders = %v", senders)
	}
}

func TestEmailRejectsInvalidAddress(t *testing.T) {
	_, _, err := emailMessage(EmailConfig{From: "from@example.com", To: []string{"not-an-address"}}, Message{Body: "hi"})
	if err == nil {
		t.Fatal("expected invalid address error")
	}
}
