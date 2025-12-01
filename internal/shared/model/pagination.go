package sharedmodel

import (
	"github.com/guregu/null/v6"
)

// PaginationParams represents the pagination parameters
type PaginationParams struct {
	Page   null.Int32  `query:"page" validate:"omitnil,gt=0"`
	Cursor null.String `query:"cursor" validate:"omitnil"`
	Limit  null.Int32  `query:"limit" validate:"omitnil,gt=0"`
}

func (p PaginationParams) Constrain() PaginationParams {
	if p.Limit.Valid {
		if p.Limit.Int32 > 100 {
			p.Limit.SetValid(100)
		}
	} else {
		p.Limit.SetValid(10)
	}

	if !p.Page.Valid {
		p.Page.SetValid(1)
	}
	return p
}

func (p PaginationParams) Offset() null.Int32 {
	if p.Limit.Valid {
		offset := (p.Page.Int32 - 1) * p.Limit.Int32
		if offset < 0 {
			return null.Int32{}
		}
		return null.Int32From(offset)
	}

	return null.Int32{}
}

// PaginateResult represents a paginated result set
type PaginateResult[T any] struct {
	PageParams PaginationParams
	Data       []T
	Total      null.Int64 // Only valid when using "page" pagination, "cursor" pagination will not
	NextCursor any        // Any struct that can be marshaled to JSON (filter conditions for the next page)
}

func (p PaginateResult[T]) NextPage() null.Int32 {
	if p.Total.Valid {
		if !p.PageParams.Limit.Valid {
			return null.Int32{}
		}

		// Default to page 1 if not set
		page := max(p.PageParams.Page.Int32, 1)
		if int64(page*p.PageParams.Limit.Int32) < p.Total.Int64 {
			return null.Int32From(p.PageParams.Page.Int32 + 1)
		}
	}
	return null.Int32{}
}
