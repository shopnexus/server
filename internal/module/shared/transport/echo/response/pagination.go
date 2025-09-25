package response

import "github.com/guregu/null/v6"

type PaginationResponse[T any] struct {
	Data     []T      `json:"data"`
	PageMeta PageMeta `json:"pagination"`
}

type PageMeta struct {
	Limit      int32       `json:"limit"`
	Total      null.Int64  `json:"total"`
	Page       null.Int32  `json:"page"`
	NextPage   null.Int32  `json:"next_page"`
	Cursor     null.String `json:"cursor"`
	NextCursor null.String `json:"next_cursor"`
}
