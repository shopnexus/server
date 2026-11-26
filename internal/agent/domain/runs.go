package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// SubjectReportCase is a conversation about an account's report case.
const SubjectReportCase SubjectKind = "report_case"

// A turn's status, as agent_turns stores it and the console reads it.
const (
	TurnRunning       = "running"
	TurnPaused        = "paused"
	TurnAwaitingInput = "awaiting_input"
	TurnSucceeded     = "succeeded"
	TurnFailed        = "failed"
	TurnCancelled     = "cancelled"
	TurnTimedOut      = "timed_out"
)

// Unfinished says whether a turn with status still holds its conversation.
func Unfinished(status string) bool {
	return status == TurnRunning || status == TurnPaused || status == TurnAwaitingInput
}

// Why a turn did not succeed, as agent_turns.error_code.
const (
	FailTimeout     = "agent_timeout"
	FailBudget      = "agent_budget"
	FailUnavailable = "agent_unavailable"
	FailFailed      = "agent_failed"
	FailLimit       = "agent_limit"
	FailContext     = "agent_context"
	FailBlocked     = "agent_blocked"
	FailStuck       = "agent_stuck"
)

// FailureAnswers are the answers stored on a turn that did not succeed.
var FailureAnswers = map[string]string{
	FailTimeout:     "Quá thời gian trả lời. Hãy hỏi lại.",
	FailBudget:      "Agent đã dùng hết ngân sách của tháng này.",
	FailUnavailable: "Agent đang không trả lời được. Hãy thử lại sau.",
	FailFailed:      "Agent gặp lỗi giữa chừng. Có thể tiếp tục lượt này.",
	FailLimit:       "Lượt hỏi đã dùng hết số bước cho phép.",
	FailContext:     "Ngữ cảnh của lượt hỏi đã đầy.",
	FailBlocked:     "Lượt hỏi bị chặn.",
	FailStuck:       "Lượt hỏi bị gián đoạn. Hãy hỏi lại.",
}

var outcomes = map[string]string{
	FailTimeout: "out_of_room", FailLimit: "out_of_room", FailContext: "out_of_room",
	FailFailed: "broken", FailStuck: "broken",
	FailUnavailable: "unavailable",
	FailBudget:      "blocked", FailBlocked: "blocked",
}

// Outcome is the group a turn's end falls in: empty on success, else
// stopped, out_of_room, broken, unavailable or blocked. A turn that has
// not ended reads as its code's group, broken without one, as the agent's
// view gave it.
func Outcome(status, errorCode string) string {
	switch status {
	case TurnSucceeded:
		return ""
	case TurnCancelled:
		return "stopped"
	case TurnTimedOut:
		return "out_of_room"
	}
	if o, ok := outcomes[errorCode]; ok {
		return o
	}
	return "broken"
}

// Continuable says whether Continue works: a paused turn always; a failed
// or timed out one when it is its conversation's latest, wrote something
// and did not stop on a spent budget, a guardrail or before writing.
func Continuable(status, errorCode string, wrote, latest bool) bool {
	switch status {
	case TurnPaused:
		return true
	case TurnTimedOut:
		errorCode = FailTimeout
	case TurnFailed:
	default:
		return false
	}
	switch errorCode {
	case FailTimeout, FailLimit, FailContext, FailFailed, FailStuck:
		return latest && wrote
	}
	return false
}

// EventStatus is turn.finished's status for a final status: upper case,
// and BUDGET for a spent budget.
func EventStatus(status, errorCode string) string {
	if errorCode == FailBudget {
		return "BUDGET"
	}
	return strings.ToUpper(status)
}

// MaxInput is the longest question or update a member may send.
const MaxInput = 2000

var (
	zeroWidth = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}]`)
	controls  = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
	tags      = regexp.MustCompile(`<[^>]*>`)
)

// CleanInput checks a question or an update and returns it without
// zero-width and control characters and HTML tags. Empty text is
// ErrRequestInvalid, text over MaxInput characters ErrInputRejected. A
// member is trusted, so phrases that read as instructions pass.
func CleanInput(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", ErrRequestInvalid.WithField("text", apperr.Required, "empty")
	}
	if utf8.RuneCountInString(text) > MaxInput {
		return "", ErrInputRejected
	}
	clean := tags.ReplaceAllString(controls.ReplaceAllString(zeroWidth.ReplaceAllString(text, ""), ""), "")
	if strings.TrimSpace(clean) == "" {
		return "", ErrRequestInvalid.WithField("text", apperr.Required, "empty")
	}
	return clean, nil
}

// Question is a clarifying question the agent asked with ask_user.
type Question struct {
	ID      string           `json:"id"`
	Label   string           `json:"label"`
	Kind    string           `json:"kind"`
	Options []QuestionOption `json:"options"`
}

// QuestionOption is one answer a question offers.
type QuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AnswerText is the member's answers as the message the model reads,
// checked against the questions: every answer names a question and its
// options, a single question takes one, and at least one is given.
func AnswerText(questions []Question, answers map[string]json.RawMessage) (string, error) {
	lines := make([]string, 0, len(questions))
	for _, q := range questions {
		raw, ok := answers[q.ID]
		if !ok {
			continue
		}
		var ids []string
		var one string
		if err := json.Unmarshal(raw, &one); err == nil {
			ids = []string{one}
		} else if err = json.Unmarshal(raw, &ids); err != nil {
			return "", ErrAnswersInvalid
		}
		if q.Kind == "single" && len(ids) != 1 {
			return "", ErrAnswersInvalid
		}
		labels := make([]string, 0, len(ids))
		for _, id := range ids {
			label, found := "", false
			for _, o := range q.Options {
				if o.ID == id {
					label, found = o.Label, true
					break
				}
			}
			if !found {
				return "", ErrAnswersInvalid
			}
			labels = append(labels, label)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", q.Label, strings.Join(labels, ", ")))
	}
	for k := range answers {
		known := false
		for _, q := range questions {
			known = known || q.ID == k
		}
		if !known {
			return "", ErrAnswersInvalid
		}
	}
	if len(lines) == 0 {
		return "", ErrAnswersInvalid
	}
	return "The member's answers to your questions:\n" + strings.Join(lines, "\n"), nil
}

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	variablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	notSlug         = regexp.MustCompile(`[^a-z0-9]+`)
)

// SlugOf is a skill's slug from its name: lower-case ASCII words joined by
// hyphens, "skill" when nothing is left.
func SlugOf(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ' || r == 'Đ':
			b.WriteRune('d')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	slug := strings.Trim(notSlug.ReplaceAllString(b.String(), "-"), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	if slug == "" {
		return "skill"
	}
	return slug
}

// SkillVariable is a blank in a skill's prompt, {{name}}.
type SkillVariable struct {
	Name    string `json:"name"`
	Label   string `json:"label,omitempty"`
	Default string `json:"default,omitempty"`
}

// CheckSkill checks a skill's fields and returns its name and prompt
// trimmed: a name of 1 to 80 characters, a slug of lower-case letters,
// digits and hyphens, a description up to 500, a prompt of 1 to 8000 and
// at most 20 variables.
func CheckSkill(name, slug, description, prompt string, vars []SkillVariable) (cleanName, cleanPrompt string, err error) {
	name, prompt = strings.TrimSpace(name), strings.TrimSpace(prompt)
	n, p := utf8.RuneCountInString(name), utf8.RuneCountInString(prompt)
	if n == 0 || n > 80 || p == 0 || p > 8000 || utf8.RuneCountInString(description) > 500 ||
		!slugPattern.MatchString(slug) || len(vars) > 20 {
		return "", "", ErrSkillInvalid
	}
	for _, v := range vars {
		if !variablePattern.MatchString(v.Name) || utf8.RuneCountInString(v.Label) > 80 || utf8.RuneCountInString(v.Default) > 500 {
			return "", "", ErrSkillInvalid
		}
	}
	return name, prompt, nil
}

// ErrRequestInvalid refuses a request the API's shape lets through but
// the agent cannot take, such as a question of spaces.
var ErrRequestInvalid = apperr.New(apperr.KindBadRequest, "invalid_request", "the request does not fit the agent")
