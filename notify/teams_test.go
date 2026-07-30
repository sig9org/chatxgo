package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTeamsCard(t *testing.T) {
	msg := Message{
		Subject:     "Deploy",
		Body:        "**done**",
		Mentions:    []Mention{{ID: "aad-id-1", Label: "Alice"}},
		Attachments: []string{"https://example.com/report.pdf"},
	}
	envelope := teamsCard(msg)

	if envelope.Type != "message" || len(envelope.Attachments) != 1 {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	att := envelope.Attachments[0]
	if att.ContentType != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("unexpected content type: %q", att.ContentType)
	}
	card := att.Content
	if card.Type != "AdaptiveCard" || card.Version == "" {
		t.Errorf("unexpected card type/version: %+v", card)
	}
	if len(card.Body) != 2 {
		t.Fatalf("expected 2 body blocks (title + text), got %+v", card.Body)
	}
	if card.Body[0].Text != "Deploy" || card.Body[0].Weight != "Bolder" || card.Body[0].Size != "Large" {
		t.Errorf("unexpected title block: %+v", card.Body[0])
	}
	if !containsAll(card.Body[1].Text, "<at>Alice</at>", "**done**", "[report.pdf](https://example.com/report.pdf)") {
		t.Errorf("unexpected text block: %q", card.Body[1].Text)
	}
	if card.MSTeams == nil || len(card.MSTeams.Entities) != 1 {
		t.Fatalf("expected 1 mention entity, got %+v", card.MSTeams)
	}
	e := card.MSTeams.Entities[0]
	if e.Type != "mention" || e.Mentioned.ID != "aad-id-1" || e.Mentioned.Name != "Alice" {
		t.Errorf("unexpected mention entity: %+v", e)
	}
}

func TestTeamsCardWithoutSubjectFallsBackToTextOnly(t *testing.T) {
	envelope := teamsCard(Message{Body: "hi"})
	card := envelope.Attachments[0].Content
	if len(card.Body) != 1 || card.Body[0].Text != "hi" {
		t.Errorf("unexpected body: %+v", card.Body)
	}
	if card.MSTeams != nil {
		t.Error("msteams entities should be omitted when there are no mentions")
	}
}

func TestTeamsSendRequiresDest(t *testing.T) {
	s := newTeamsSender(TeamsConfig{})
	if err := s.Send(context.Background(), Message{Body: "hi"}); err == nil {
		t.Error("expected error when MSTEAMS_DST is missing")
	}
}

func TestTeamsSendPostsCard(t *testing.T) {
	var got teamsMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %q", ct)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newTeamsSender(TeamsConfig{Dest: srv.URL})
	if err := s.Send(context.Background(), Message{Subject: "hi", Body: "body"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Content.Body[0].Text != "hi" {
		t.Errorf("unexpected posted card: %+v", got)
	}
}

func TestTeamsSendErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	s := newTeamsSender(TeamsConfig{Dest: srv.URL})
	if err := s.Send(context.Background(), Message{Body: "hi"}); err == nil {
		t.Error("expected error on non-2xx response")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
