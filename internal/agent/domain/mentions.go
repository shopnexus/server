package domain

import (
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"
)

// Attachment is a record a member mentioned in a question: its kind, its
// id and the label the console showed for it.
type Attachment struct {
	Kind  string    `json:"kind"`
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label,omitempty"`
}

// AttachmentKinds are the records a question may mention.
var AttachmentKinds = []string{"ticket", "account", "order", "listing", "report_case", "document"}

const (
	// MaxAttachments bounds a question's attachments, and MaxAttachmentLabel
	// the characters of a label.
	MaxAttachments     = 10
	MaxAttachmentLabel = 120
	// MaxMentions bounds the records a run is told the conversation
	// mentions.
	MaxMentions = 20
)

// CheckAttachments returns a question's attachments with each record once,
// the first kept, and their labels trimmed. An unknown kind, more than
// MaxAttachments or a label over MaxAttachmentLabel is ErrRequestInvalid.
func CheckAttachments(in []Attachment) ([]Attachment, error) {
	if len(in) > MaxAttachments {
		return nil, ErrRequestInvalid
	}
	out := make([]Attachment, 0, len(in))
	for _, a := range in {
		a.Label = strings.TrimSpace(a.Label)
		if !slices.Contains(AttachmentKinds, a.Kind) || utf8.RuneCountInString(a.Label) > MaxAttachmentLabel {
			return nil, ErrRequestInvalid
		}
		if !slices.ContainsFunc(out, func(b Attachment) bool { return b.Kind == a.Kind && b.ID == a.ID }) {
			out = append(out, a)
		}
	}
	return out, nil
}
