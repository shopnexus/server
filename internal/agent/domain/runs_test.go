package domain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/agent/domain"
)

// A turn's outcome, whether Continue works and turn.finished's status, as
// the agent worked them out.
func TestOutcomeAndContinuable(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		status, code, outcome string
		wrote, latest, cont   bool
	}{
		{domain.TurnSucceeded, "", "", true, true, false},
		{domain.TurnCancelled, "", "stopped", true, true, false},
		{domain.TurnTimedOut, "", "out_of_room", true, true, true},
		{domain.TurnTimedOut, "", "out_of_room", true, false, false},
		{domain.TurnFailed, domain.FailStuck, "broken", true, true, true},
		{domain.TurnFailed, domain.FailStuck, "broken", false, true, false},
		{domain.TurnFailed, domain.FailLimit, "out_of_room", true, true, true},
		{domain.TurnFailed, domain.FailBudget, "blocked", true, true, false},
		{domain.TurnFailed, domain.FailUnavailable, "unavailable", true, true, false},
		{domain.TurnFailed, "", "broken", true, true, false},
		{domain.TurnPaused, "", "broken", false, false, true},
		{domain.TurnAwaitingInput, "", "broken", true, true, false},
	} {
		if got := domain.Outcome(c.status, c.code); got != c.outcome {
			t.Errorf("outcome %s %s: %q, want %q", c.status, c.code, got, c.outcome)
		}
		if got := domain.Continuable(c.status, c.code, c.wrote, c.latest); got != c.cont {
			t.Errorf("continuable %+v: %v", c, got)
		}
	}
	if domain.EventStatus(domain.TurnFailed, domain.FailBudget) != "BUDGET" || domain.EventStatus(domain.TurnTimedOut, "") != "TIMED_OUT" {
		t.Error("event status")
	}
}

// The input guard refuses empty and long text and takes out zero-width
// and control characters and tags.
func TestCleanInput(t *testing.T) {
	t.Parallel()
	got, err := domain.CleanInput("Ai\u200b mở <b>ticket</b>?\x07")
	if err != nil || got != "Ai mở ticket?" {
		t.Errorf("clean %q %v", got, err)
	}
	for _, text := range []string{"", "   ", "<p></p>\u200b"} {
		if _, err = domain.CleanInput(text); !errors.Is(err, domain.ErrRequestInvalid) {
			t.Errorf("%q: %v", text, err)
		}
	}
	if _, err = domain.CleanInput(strings.Repeat("ạ", domain.MaxInput)); err != nil {
		t.Errorf("2000 characters: %v", err)
	}
	if _, err = domain.CleanInput(strings.Repeat("a", domain.MaxInput+1)); !errors.Is(err, domain.ErrInputRejected) {
		t.Errorf("2001 characters: %v", err)
	}
}

// The answers become one line per question with the options' labels, and
// answers that do not fit the questions are refused.
func TestAnswerText(t *testing.T) {
	t.Parallel()
	questions := []domain.Question{
		{ID: "range", Label: "Khoảng thời gian", Kind: "single", Options: []domain.QuestionOption{{"week", "Tuần này"}, {"month", "Tháng này"}}},
		{ID: "cities", Label: "Thành phố", Kind: "multi", Options: []domain.QuestionOption{{"hn", "Hà Nội"}, {"hcm", "TP.HCM"}}},
	}
	raw := func(s string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	got, err := domain.AnswerText(questions, raw(`{"range":"week","cities":["hn","hcm"]}`))
	want := "The member's answers to your questions:\n- Khoảng thời gian: Tuần này\n- Thành phố: Hà Nội, TP.HCM"
	if err != nil || got != want {
		t.Errorf("%q %v", got, err)
	}
	for _, bad := range []string{`{}`, `{"range":"year"}`, `{"range":["week","month"]}`, `{"other":"x"}`, `{"range":1}`} {
		if _, err = domain.AnswerText(questions, raw(bad)); !errors.Is(err, domain.ErrAnswersInvalid) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// A skill's slug is its name in lower-case ASCII.
func TestSlugOf(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{"Báo cáo tuần": "bao-cao-tuan", "Đơn hàng  #1": "don-hang-1", "!!!": "skill"} {
		if got := domain.SlugOf(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}
