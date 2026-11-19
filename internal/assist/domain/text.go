package domain

import (
	"strings"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// NoItem is the whole reply of a model that sees no item for sale.
const NoItem = "NO_ITEM"

// Text is what the model writes for a read: its first line is the title,
// the lines after it the description.
type Text struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// SplitText reads raw, whole or cut short, as a Text: the title squeezed
// and cut to the listing's, the description trimmed and cut to
// maxDescription. A reply that may still turn out to be NoItem is empty.
func SplitText(raw string, maxDescription int) Text {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(NoItem, raw) || strings.HasPrefix(raw, NoItem) {
		return Text{}
	}
	// The model sometimes labels the lines although told not to.
	raw = strings.TrimSpace(dropLabel(raw, titleLabels))
	title, description, _ := strings.Cut(raw, "\n")
	description = strings.TrimSpace(dropLabel(strings.TrimSpace(description), descriptionLabels))
	return Text{
		Title:       cut(strings.Join(strings.Fields(title), " "), catalogdomain.MaxTitle),
		Description: cut(strings.TrimSpace(description), maxDescription),
	}
}

var (
	titleLabels       = []string{"Tiêu đề:", "Tên:", "Title:"}
	descriptionLabels = []string{"Mô tả:", "Description:"}
)

// dropLabel takes one of labels off the start of s, case aside.
func dropLabel(s string, labels []string) string {
	for _, l := range labels {
		if len(s) >= len(l) && strings.EqualFold(s[:len(l)], l) {
			return s[len(l):]
		}
	}
	return s
}

// textDraft is the draft of a whole reply: its title and description,
// used, with no specs and no photos. NoItem, or no title, is ErrNoItem.
func textDraft(raw string, maxDescription int) (Draft, error) {
	t := SplitText(raw, maxDescription)
	if t.Title == "" {
		return Draft{}, ErrNoItem
	}
	return Draft{
		Title: t.Title, Description: t.Description, Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{}, Photos: []uuid.UUID{},
	}, nil
}

// VideoDraft is the draft of a video read's reply. Its photos are the
// sharpest frame, the cover, then the others in time order, MaxKept in all.
func VideoDraft(raw string, frames []Photo, sharpest int) (Draft, error) {
	d, err := textDraft(raw, catalogdomain.MaxDescription)
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
