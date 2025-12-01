package commonmodel

import (
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type Resource struct {
	ID   uuid.UUID `json:"id"`
	Url  string    `json:"url"`
	Mime string    `json:"mime"`
	Size int64     `json:"size"`
	// Width    null.Int32  `json:"width"`
	// Height   null.Int32  `json:"height"`
	// Duration null.Float  `json:"duration"`
	Checksum null.String `json:"checksum"`
}
