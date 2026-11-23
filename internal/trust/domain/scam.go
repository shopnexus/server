package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"
)

// Code names one sign of a scam. A classifier reads the message codes from
// the messages; a fact code is computed from data and joins a warning only
// beside a message code.
type Code string

const (
	CodePrepayment    Code = "prepayment_request"
	CodeOffApp        Code = "off_app_contact"
	CodeLink          Code = "suspicious_link"
	CodeSensitive     Code = "sensitive_info_request"
	CodeImpersonation Code = "impersonation"
	CodeFakeProof     Code = "fake_payment_proof"
	CodePressure      Code = "pressure"
	CodeNewAccount    Code = "new_account"
	CodePriceBelow    Code = "price_far_below"
)

// MessageCodes are the codes a classifier may answer, in the order a
// warning lists the ones found at the same check.
var MessageCodes = []Code{
	CodePrepayment, CodeOffApp, CodeLink, CodeSensitive, CodeImpersonation, CodeFakeProof, CodePressure,
}

// FromMessages reports whether c is one of MessageCodes.
func (c Code) FromMessages() bool { return slices.Contains(MessageCodes, c) }

const (
	// WindowSize is how many of the newest messages with text a check reads.
	WindowSize = 20
	// ChecksPerDay caps the checks of one conversation in 24 hours.
	ChecksPerDay = 50
	// CheckRetention is how long a check is kept.
	CheckRetention = 365 * 24 * time.Hour
	// NewAccountAge is the age under which an account is new.
	NewAccountAge = 7 * 24 * time.Hour
	// PriceGapMin is the percentage under the median from which a price is
	// far below the market, over at least PriceSampleMin listings.
	PriceGapMin    = 30
	PriceSampleMin = 5
)

// Signal is one code of a warning, with the parameters of its text, when a
// check first found it and the suspect's newest message in that check's
// window, which signals saved before 2026-11-20 lack.
type Signal struct {
	Code        Code           `json:"code"`
	Params      map[string]int `json:"params,omitempty"`
	FirstSeenAt time.Time      `json:"first_seen_at"`
	MessageID   *uuid.UUID     `json:"message_id,omitempty"`
}

// NewAccountSignal is new_account for an account created at created, when
// it is under NewAccountAge old at now; days is whole days, at least 1.
func NewAccountSignal(created, now time.Time) (Signal, bool) {
	age := now.Sub(created)
	if age < 0 || age >= NewAccountAge {
		return Signal{}, false
	}
	return Signal{Code: CodeNewAccount, Params: map[string]int{"days": max(1, int(age/(24*time.Hour)))}}, true
}

// PriceBelowSignal is price_far_below for a price at least PriceGapMin
// percent under median, over a sample of at least PriceSampleMin listings;
// percent is rounded down.
func PriceBelowSignal(price, median int64, size int) (Signal, bool) {
	if size < PriceSampleMin || median <= 0 || price >= median {
		return Signal{}, false
	}
	percent := int((median - price) * 100 / median)
	if percent < PriceGapMin {
		return Signal{}, false
	}
	return Signal{Code: CodePriceBelow, Params: map[string]int{"percent": percent}}, true
}

// Text is the signal's text in t, with its params; empty for an unknown
// code.
func (s Signal) Text(t locale.Tag) string {
	switch s.Code {
	case CodePrepayment:
		return say(t, "signal.prepayment_request")
	case CodeOffApp:
		return say(t, "signal.off_app_contact")
	case CodeLink:
		return say(t, "signal.suspicious_link")
	case CodeSensitive:
		return say(t, "signal.sensitive_info_request")
	case CodeImpersonation:
		return say(t, "signal.impersonation")
	case CodeFakeProof:
		return say(t, "signal.fake_payment_proof")
	case CodePressure:
		return say(t, "signal.pressure")
	case CodeNewAccount:
		if days := s.Params["days"]; days != 1 {
			return say(t, "signal.new_account.other", days)
		}
		return say(t, "signal.new_account.one", 1)
	case CodePriceBelow:
		return say(t, "signal.price_far_below", s.Params["percent"])
	default:
		return ""
	}
}

// Side is a member's place in a conversation: a for account_a, b for
// account_b.
type Side string

const (
	SideA Side = "a"
	SideB Side = "b"
)

// Line is one message of a window: who sent it, its text after Redact and
// the listing it names.
type Line struct {
	MessageID uuid.UUID
	Side      Side
	Text      string
	ListingID *uuid.UUID
}

// Window is what a classifier reads: a conversation's newest messages with
// text, oldest first.
type Window struct {
	Lines []Line
}

// Verdict is a classifier's answer: the message codes of each side's own
// messages.
type Verdict struct {
	A []Code `json:"a"`
	B []Code `json:"b"`
}

// Clean keeps the message codes of each side once, in the order given, and
// counts what it dropped: unknown codes, fact codes and repeats.
func (v Verdict) Clean() (clean Verdict, dropped int) {
	keep := func(codes []Code) []Code {
		out := []Code{}
		for _, c := range codes {
			if !c.FromMessages() || slices.Contains(out, c) {
				dropped++
				continue
			}
			out = append(out, c)
		}
		return out
	}
	return Verdict{A: keep(v.A), B: keep(v.B)}, dropped
}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// urlPattern is a link with a scheme and any host (group 1), or a bare
	// host under a common top-level domain (group 2), in one pass so a
	// placeholder is never matched again.
	urlPattern = regexp.MustCompile(`(?i)\bhttps?://([^\s/?#]+)\S*|` +
		`\b((?:[a-z0-9-]+\.)+(?:com|net|org|info|vn|me|ly|io|co|xyz|top|site|online|shop|link|app|cc|tk))\b(?:[/?#]\S*)?`)
	// runPattern is a run of digits with single spaces, dots or dashes
	// between them, after an optional plus sign.
	runPattern = regexp.MustCompile(`\+?\d(?:[\s.\-]?\d)*`)
	// unitPattern is a currency unit right after a run, with one optional
	// space before it.
	unitPattern = regexp.MustCompile(`(?i)^\s?(vnđ|vnd|đ|k|tr)`)
)

// amountDigits caps the digits of a run read as an amount before each
// unit: up to 1,000,000,000 đồng, or 9,999,999 with k or tr.
var amountDigits = map[string]int{"vnđ": 10, "vnd": 10, "đ": 10, "k": 7, "tr": 7}

// Redact replaces email addresses, links, phone numbers and runs of 8 to 19
// digits (bank accounts, cards) with <EMAIL>, <URL:domain>, <PHONE> and
// <BANK_NUMBER>. A run is an amount, and stays, when a currency unit follows
// it that is not the start of a word and it has no more digits than the
// unit allows. A run is a phone when it starts with +, or has 10 or 11
// digits from 0, or 11 from 84.
func Redact(text string) string {
	text = emailPattern.ReplaceAllString(text, "<EMAIL>")
	text = urlPattern.ReplaceAllStringFunc(text, func(m string) string {
		g := urlPattern.FindStringSubmatch(m)
		host := g[1]
		if host == "" {
			host = g[2]
		}
		return "<URL:" + strings.TrimPrefix(strings.ToLower(host), "www.") + ">"
	})
	var b strings.Builder
	last := 0
	for _, loc := range runPattern.FindAllStringIndex(text, -1) {
		run := text[loc[0]:loc[1]]
		digits := strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, run)
		n := len(digits)
		after := text[loc[1]:]
		if u := unitPattern.FindStringSubmatch(after); u != nil {
			next, _ := utf8.DecodeRuneInString(after[len(u[0]):])
			wordGoesOn := unicode.IsLetter(next) || unicode.IsDigit(next)
			if !wordGoesOn && n <= amountDigits[strings.ToLower(u[1])] {
				continue
			}
		}
		var placeholder string
		switch {
		case strings.HasPrefix(run, "+") && n >= 9,
			strings.HasPrefix(digits, "0") && (n == 10 || n == 11),
			strings.HasPrefix(digits, "84") && n == 11:
			placeholder = "<PHONE>"
		case n >= 8 && n <= 19:
			placeholder = "<BANK_NUMBER>"
		default:
			continue
		}
		b.WriteString(text[last:loc[0]])
		b.WriteString(placeholder)
		last = loc[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// Warning is what one member of a conversation is told about the other
// member, the suspect: the signals found, in the order found, and when the
// member last dismissed it.
type Warning struct {
	conversationID uuid.UUID
	accountID      uuid.UUID
	suspectID      uuid.UUID
	signals        []Signal
	dismissedAt    *time.Time
	updatedAt      time.Time
	writeID        uuid.UUID
}

// WarningState is a Warning's fields, for repositories.
type WarningState struct {
	ConversationID uuid.UUID
	AccountID      uuid.UUID
	SuspectID      uuid.UUID
	Signals        []Signal
	DismissedAt    *time.Time
	UpdatedAt      time.Time
	WriteID        uuid.UUID
}

// NewWarning starts account's warning about suspect in conversation, with
// no signal and no write ID.
func NewWarning(conversation, account, suspect uuid.UUID) Warning {
	return Warning{conversationID: conversation, accountID: account, suspectID: suspect}
}

// RestoreWarning rebuilds a stored warning; only repositories call it.
func RestoreWarning(st WarningState) Warning {
	return Warning{
		conversationID: st.ConversationID, accountID: st.AccountID, suspectID: st.SuspectID,
		signals: slices.Clone(st.Signals), dismissedAt: st.DismissedAt, updatedAt: st.UpdatedAt, writeID: st.WriteID,
	}
}

func (w *Warning) State() WarningState {
	return WarningState{
		ConversationID: w.conversationID, AccountID: w.accountID, SuspectID: w.suspectID,
		Signals: slices.Clone(w.signals), DismissedAt: w.dismissedAt, UpdatedAt: w.updatedAt, WriteID: w.writeID,
	}
}

// Saved records that the warning was written under writeID; only
// repositories call it.
func (w *Warning) Saved(writeID uuid.UUID) { w.writeID = writeID }

func (w *Warning) ConversationID() uuid.UUID { return w.conversationID }
func (w *Warning) AccountID() uuid.UUID      { return w.accountID }
func (w *Warning) SuspectID() uuid.UUID      { return w.suspectID }
func (w *Warning) Signals() []Signal         { return slices.Clone(w.signals) }
func (w *Warning) DismissedAt() *time.Time   { return w.dismissedAt }
func (w *Warning) UpdatedAt() time.Time      { return w.updatedAt }
func (w *Warning) WriteID() uuid.UUID        { return w.writeID }

// has reports whether the warning holds code c.
func (w *Warning) has(c Code) bool {
	return slices.ContainsFunc(w.signals, func(s Signal) bool { return s.Code == c })
}

// Raise adds the message codes of messages the warning lacks, in the order
// of MessageCodes, then the fact signals of facts it lacks, each first seen
// at the time at, and found at message, the suspect's newest message in the
// check's window (nil when it has none there), and reports whether the
// warning changed. Without a message code in messages it adds nothing. A check
// reads its clock before it calls the model, so a dismissal can land after
// at; the codes are then first seen a microsecond after the dismissal,
// since the member dismissed without them.
func (w *Warning) Raise(messages []Code, facts []Signal, at time.Time, message *uuid.UUID) bool {
	if !slices.ContainsFunc(messages, Code.FromMessages) {
		return false
	}
	if w.dismissedAt != nil && !at.After(*w.dismissedAt) {
		at = w.dismissedAt.Add(time.Microsecond)
	}
	changed := false
	for _, c := range MessageCodes {
		if slices.Contains(messages, c) && !w.has(c) {
			w.signals = append(w.signals, Signal{Code: c, FirstSeenAt: at, MessageID: message})
			changed = true
		}
	}
	for _, f := range facts {
		if !f.Code.FromMessages() && !w.has(f.Code) {
			f.FirstSeenAt, f.MessageID = at, message
			w.signals = append(w.signals, f)
			changed = true
		}
	}
	if changed {
		w.updatedAt = at
	}
	return changed
}

// Dismissed holds after a dismissal that no message code has been found
// since.
func (w *Warning) Dismissed() bool {
	if w.dismissedAt == nil {
		return false
	}
	return !slices.ContainsFunc(w.signals, func(s Signal) bool {
		return s.Code.FromMessages() && s.FirstSeenAt.After(*w.dismissedAt)
	})
}

// Dismiss records a dismissal at at. A warning without signals, or one
// dismissed already, does not change; Dismiss reports whether it changed.
func (w *Warning) Dismiss(at time.Time) bool {
	if len(w.signals) == 0 || w.Dismissed() {
		return false
	}
	w.dismissedAt, w.updatedAt = &at, at
	return true
}

// Check is one classifier run over a window of a conversation: the
// window's messages, oldest first, which classifier and prompt read it, and
// the codes of each side after Clean, with no message text.
type Check struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	MessageIDs     []uuid.UUID
	Classifier     string
	PromptVersion  int
	A, B           []Code
	CreatedAt      time.Time
}
