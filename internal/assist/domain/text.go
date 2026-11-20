package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// MaxReason is the longest reason a failed read keeps for the seller.
const MaxReason = 300

// Reply is the model's answer for a read, in the shape ReplySchema asks
// for: what the source shows, whether it is one item for sale, the title
// and description, and, when it is not, why, in a sentence for the seller.
type Reply struct {
	Subject     string `json:"subject"`
	Found       bool   `json:"found"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

// ReplySchema is Reply's JSON schema. The fields are in the order the model
// writes them, so it names the subject before it decides.
var ReplySchema = json.RawMessage(`{"type":"object","additionalProperties":false,` +
	`"required":["subject","found","title","description","reason"],"properties":{` +
	`"subject":{"type":"string"},"found":{"type":"boolean"},"title":{"type":"string"},` +
	`"description":{"type":"string"},"reason":{"type":"string"}}}`)

// ReplySchemaName names ReplySchema in the request.
const ReplySchemaName = "listing_read"

// ParseReply reads the model's whole answer. Text or a code fence around
// the object is dropped.
func ParseReply(raw string) (Reply, error) {
	start, end := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return Reply{}, fmt.Errorf("%w: no JSON object", ErrBadReply)
	}
	var r Reply
	if err := json.Unmarshal([]byte(raw[start:end+1]), &r); err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrBadReply, err)
	}
	return r, nil
}

// ErrBadReply is an answer that is not the JSON ReplySchema asks for.
var ErrBadReply = errors.New("assist: the model's answer is not the reply's JSON")

// NoItemError is a reply that shows no item for sale, with the model's
// reason; [errors.Is] matches it to ErrNoItem.
type NoItemError struct{ Reason string }

func (NoItemError) Error() string        { return ErrNoItem.Error() }
func (NoItemError) Is(target error) bool { return target == ErrNoItem }

// Text is the title and description the model has written for a read.
type Text struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// cleanText squeezes the title and cuts it to the listing's, and trims the
// description and cuts it to maxDescription.
func cleanText(title, description string, maxDescription int) Text {
	return Text{
		Title:       cut(strings.Join(strings.Fields(title), " "), catalogdomain.MaxTitle),
		Description: cut(strings.TrimSpace(description), maxDescription),
	}
}

// PartialText is the text of an answer the model is still writing. It is
// empty until the answer says it found an item.
func PartialText(raw string, maxDescription int) Text {
	v := scanPartial(raw)
	if found, ok := v["found"].(bool); !ok || !found {
		return Text{}
	}
	title, ok := v["title"].(string)
	if !ok {
		return Text{}
	}
	description, ok := v["description"].(string)
	if !ok {
		description = ""
	}
	return cleanText(title, description, maxDescription)
}

// textDraft is the draft of a whole reply: its title and description,
// used, with no specs and no photos. A reply that found nothing, or has no
// title, is a NoItemError.
func textDraft(r Reply, maxDescription int) (Draft, error) {
	t := cleanText(r.Title, r.Description, maxDescription)
	if !r.Found || t.Title == "" {
		return Draft{}, NoItemError{Reason: r.Reason}
	}
	return Draft{
		Title: t.Title, Description: t.Description, Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{}, Photos: []uuid.UUID{},
	}, nil
}

// VideoDraft is the draft of a video read's reply. Its photos are the
// sharpest frame, the cover, then the others in time order, MaxKept in all.
func VideoDraft(r Reply, frames []Photo, sharpest int) (Draft, error) {
	d, err := textDraft(r, catalogdomain.MaxDescription)
	if err != nil {
		return Draft{}, err
	}
	if sharpest >= 0 && sharpest < len(frames) {
		d.Photos = append(d.Photos, frames[sharpest].UploadID)
	}
	for i, f := range frames {
		if i != sharpest && len(d.Photos) < MaxKept {
			d.Photos = append(d.Photos, f.UploadID)
		}
	}
	return d, nil
}

// scanPartial reads the string and boolean values of a JSON object that
// may be cut short; a string cut short is kept as far as it goes.
func scanPartial(raw string) map[string]any {
	out := map[string]any{}
	i := strings.IndexByte(raw, '{')
	if i < 0 {
		return out
	}
	for i++; ; {
		i = skipSpace(raw, i)
		if i < len(raw) && raw[i] == ',' {
			i = skipSpace(raw, i+1)
		}
		if i >= len(raw) || raw[i] != '"' {
			return out
		}
		key, next, closed := readString(raw, i)
		if !closed {
			return out
		}
		i = skipSpace(raw, next)
		if i >= len(raw) || raw[i] != ':' {
			return out
		}
		i = skipSpace(raw, i+1)
		switch {
		case i >= len(raw):
			return out
		case raw[i] == '"':
			s, next, closed := readString(raw, i)
			out[key] = s
			if !closed {
				return out
			}
			i = next
		case strings.HasPrefix(raw[i:], "true"):
			out[key], i = true, i+4
		case strings.HasPrefix(raw[i:], "false"):
			out[key], i = false, i+5
		default:
			return out
		}
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) && strings.IndexByte(" \t\r\n", s[i]) >= 0 {
		i++
	}
	return i
}

// readString decodes the JSON string that starts at s[i], a quote. Cut
// short, it is what decodes so far, and closed is false.
func readString(s string, i int) (v string, next int, closed bool) {
	end := len(s)
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			// An escape cut short is dropped.
			n := 2
			if j+1 < len(s) && s[j+1] == 'u' {
				n = 6
			}
			if j+n > len(s) {
				end = j
			}
			j += n - 1
		case '"':
			_ = json.Unmarshal([]byte(s[i:j+1]), &v)
			return v, j + 1, true
		default:
		}
	}
	_ = json.Unmarshal([]byte(s[i:min(end, len(s))]+`"`), &v)
	return v, len(s), false
}
