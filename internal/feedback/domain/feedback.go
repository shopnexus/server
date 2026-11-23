// Package domain holds module feedback's rules: what feedback needs, what
// staff may set on a group, and how much a person sends in a day.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

var (
	ErrInvalid  = apperr.New(apperr.KindInvalid, "invalid_feedback", "the feedback breaks a rule; see fields")
	ErrTooMany  = apperr.New(apperr.KindRateLimited, "too_many_feedback", "at most 20 feedback in 24 hours")
	ErrNotFound = apperr.New(apperr.KindNotFound, "feedback_not_found", "no such feedback")
)

// Kind is what the person says the feedback is.
type Kind string

const (
	KindBug    Kind = "bug"
	KindIdea   Kind = "idea"
	KindPraise Kind = "praise"
)

// Status is how far ShopNexus got with a feedback's group.
type Status string

const (
	StatusSent       Status = "sent"
	StatusSeen       Status = "seen"
	StatusInProgress Status = "in_progress"
	StatusShipped    Status = "shipped"
)

// Tells reports whether a move to s tells the feedback's owner.
func (s Status) Tells() bool { return s == StatusInProgress || s == StatusShipped }

const (
	// DailyLimit is how much feedback an account sends in Window.
	DailyLimit = 20
	Window     = 24 * time.Hour
	// MaxText is the most characters of a feedback's text and of the reply.
	MaxText   = 1000
	MaxPhotos = 3
	maxNote   = 80
	maxVer    = 20
)

// Device is the phone a feedback was sent from.
type Device struct {
	Model      string `json:"model"`
	OS         string `json:"os"`
	AppVersion string `json:"app_version"`
}

// Photo is one processed screenshot, by the upload it came from.
type Photo struct {
	UploadID uuid.UUID `json:"upload_id"`
	PhotoKey string    `json:"photo_key"`
	ThumbKey string    `json:"thumb_key"`
}

// Draft is feedback as its person sent it, checked. Mood 0 and an empty
// Kind or Text mean none.
type Draft struct {
	Mood    int
	Kind    Kind
	Text    string
	Photos  []uuid.UUID
	Device  *Device
	Screen  string
	Contact bool
}

// NewFeedback checks feedback: it needs a mood or a text that is not
// blank. The text is trimmed. Every broken field is named.
func NewFeedback(mood int, kind, text string, photos []uuid.UUID, device *Device, screen string, contact bool) (Draft, error) {
	e := ErrInvalid
	d := Draft{
		Mood: mood, Kind: Kind(kind), Text: strings.TrimSpace(text), Photos: photos, Device: device,
		Screen: strings.TrimSpace(screen), Contact: contact,
	}
	if mood < 0 || mood > 5 {
		e = e.WithField("mood", apperr.OutOfRange, "1 to 5")
	}
	switch d.Kind {
	case "", KindBug, KindIdea, KindPraise:
	default:
		e = e.WithField("kind", apperr.NotAllowed, "bug, idea or praise")
	}
	if n := utf8.RuneCountInString(d.Text); n > MaxText {
		e = e.WithField("text", apperr.TooLong, "at most 1,000 characters")
	}
	if mood == 0 && d.Text == "" {
		e = e.WithField("text", apperr.Required, "a mood or a text")
	}
	if len(photos) > MaxPhotos {
		e = e.WithField("photos", apperr.TooMany, "at most 3 photos")
	}
	if len(e.Fields) > 0 {
		return Draft{}, e
	}
	return d, nil
}

// StatusChange is what staff set on a group: a status with its note and,
// for shipped, the version. An empty note or version is none.
type StatusChange struct {
	Status  Status
	Note    string
	Version string
}

// NewStatusChange checks a status change; note and version are trimmed,
// and a version needs shipped.
func NewStatusChange(status, note, version string) (StatusChange, error) {
	e := ErrInvalid
	c := StatusChange{Status: Status(status), Note: strings.TrimSpace(note), Version: strings.TrimSpace(version)}
	switch c.Status {
	case StatusSent, StatusSeen, StatusInProgress, StatusShipped:
	default:
		e = e.WithField("status", apperr.NotAllowed, "sent, seen, in_progress or shipped")
	}
	if utf8.RuneCountInString(c.Note) > maxNote {
		e = e.WithField("note", apperr.TooLong, "at most 80 characters")
	}
	if utf8.RuneCountInString(c.Version) > maxVer {
		e = e.WithField("version", apperr.TooLong, "at most 20 characters")
	}
	if c.Version != "" && c.Status != StatusShipped {
		e = e.WithField("version", apperr.NotAllowed, "only with shipped")
	}
	if len(e.Fields) > 0 {
		return StatusChange{}, e
	}
	return c, nil
}

// ReplyText checks ShopNexus's reply: trimmed, 1 to 1,000 characters.
func ReplyText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxText {
		return "", ErrInvalid.WithField("text", apperr.Length(n, 1, MaxText), "1 to 1,000 characters")
	}
	return text, nil
}
