package domain

import (
	"encoding/json"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// MaxPhotoDescription is the longest description a photo read writes: a
// sentence or two of what the photo shows.
const MaxPhotoDescription = 300

// PhotoAnswer is what the model returns for one photo, in
// PhotoAnswerSchema's shape. A photo has no spoken price and no frames to
// keep.
type PhotoAnswer struct {
	IsItem      bool                 `json:"is_item"`
	Title       string               `json:"title"`
	Condition   string               `json:"condition"`
	Specs       []catalogdomain.Spec `json:"specs"`
	Description string               `json:"description"`
	Estimate    *Range               `json:"estimate"`
}

// PhotoAnswerSchemaName names PhotoAnswerSchema in the request.
const PhotoAnswerSchemaName = "photo_read"

// PhotoAnswerSchema is PhotoAnswer as a strict JSON schema.
var PhotoAnswerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,
"required":["is_item","title","condition","specs","description","estimate"],
"properties":{"is_item":{"type":"boolean"},"title":{"type":"string"},
"condition":{"type":"string","enum":["new","like_new","used"]},
"specs":{"type":"array","maxItems":20,"items":{"type":"object","additionalProperties":false,"required":["label","value"],
"properties":{"label":{"type":"string"},"value":{"type":"string"}}}},
"description":{"type":"string"},
"estimate":{"anyOf":[{"type":"null"},{"type":"object","additionalProperties":false,"required":["low","median","high"],
"properties":{"low":{"type":"integer"},"median":{"type":"integer"},"high":{"type":"integer"}}}]}}}`)

// CleanPhoto turns the model's answer for photo into a draft whose one
// photo is photo, with no price, and the model's range. The description is
// cut at MaxPhotoDescription. No item, or no title, is ErrNoItem.
func CleanPhoto(a PhotoAnswer, photo uuid.UUID) (Draft, *Range, error) {
	d, err := cleanDraft(a.IsItem, a.Title, a.Condition, a.Specs, a.Description)
	if err != nil {
		return Draft{}, nil, err
	}
	d.Description = cut(d.Description, MaxPhotoDescription)
	d.Photos = []uuid.UUID{photo}
	return d, cleanRange(a.Estimate), nil
}
