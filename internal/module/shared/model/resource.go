package sharedmodel

import (
	"github.com/guregu/null/v6"
)

type CreateResource struct {
	FileID int64 `json:"file_id" validate:"required"`
}

type Resource struct {
	ID       int64       `json:"id"`
	Url      string      `json:"url"`
	Mime     string      `json:"mime"`
	FileSize null.Int64  `json:"file_size"`
	Width    null.Int32  `json:"width"`
	Height   null.Int32  `json:"height"`
	Duration null.Float  `json:"duration"`
	Checksum null.String `json:"checksum"`
}
