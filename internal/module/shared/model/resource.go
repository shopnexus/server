package sharedmodel

import (
	"github.com/guregu/null/v6"
)

type CreateResource struct {
	Order int32  `json:"order" validate:"required,gte=0"`
	Url   string `json:"url" validate:"required,url"`
}

type Resource struct {
	ID       int64      `json:"id"`
	Mime     string     `json:"mime"`
	Url      string     `json:"url"`
	FileSize null.Int64 `json:"file_size"`
	Width    null.Int32 `json:"width"`
	Height   null.Int32 `json:"height"`
	Duration null.Float `json:"duration"`
}
