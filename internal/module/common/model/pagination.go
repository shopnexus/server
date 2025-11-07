package commonmodel

import (
	"strconv"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
)

// PaginationParams represents the pagination parameters
type PaginationParams struct {
	Page   null.Int32  `query:"page" validate:"omitnil,gt=0"`
	Cursor null.String `query:"cursor" validate:"omitnil"`
	Limit  int32       `query:"limit" validate:"omitempty,gt=0,lte=100"`
}

func (p *PaginationParams) Offset() int32 {
	if p.Limit <= 0 {
		p.Limit = 10 // default limit
	}

	// Page pagination
	if p.Page.Valid {
		if p.Page.Int32 <= 0 {
			p.Page.SetValid(1)
		}

		offset := (p.Page.Int32 - 1) * p.Limit
		if offset < 0 {
			return 0
		}
		return offset
	}

	if p.Cursor.Valid {
		return 0
	}

	return 0
}

func (p *PaginationParams) GetPage() int32 {
	if p.Page.Int32 <= 0 {
		p.Page.SetValid(1)
	}
	return p.Page.Int32
}

func (p *PaginationParams) GetLimit() int32 {
	if p.Limit <= 0 {
		p.Limit = 10 // default limit
	}
	return p.Limit
}

func (p *PaginationParams) GetCursorID() pgtype.Int8 {
	var zero pgtype.Int8
	if p.Cursor.Valid {
		id, err := strconv.ParseInt(p.Cursor.String, 10, 64)
		if err == nil {
			return pgtype.Int8{Int64: id, Valid: true}
		}
	}
	return zero
}

//func (p *PaginationParams) NextPage(total int64) *int32 {
//	if p.Page <= 0 {
//		p.Page = 0
//	}
//	if p.Limit <= 0 {
//		p.Limit = 10
//	}
//
//	if int64(p.Page*p.Limit) < total {
//		nextPage := p.Page + 1
//		return &nextPage
//	}
//
//	return nil
//}
//
//func (p *PaginationParams) NextCursor(lastID int64) *string {
//
//	// Cursor pagination is not implemented yet
//	return nil
//}

// PaginateResult represents a paginated result set
type PaginateResult[T any] struct {
	PageParams PaginationParams
	Data       []T
	Total      null.Int64 // Only valid when using "page" pagination, "cursor" pagination will not
	NextCursor any        // Any struct that can be marshaled to JSON (filter conditions for the next page)
}

func (p PaginateResult[T]) NextPage() null.Int32 {
	if p.Total.Valid {
		if int64(p.PageParams.GetPage()*p.PageParams.GetLimit()) < p.Total.Int64 {
			return null.Int32From(p.PageParams.Page.Int32 + 1)
		}
	}
	return null.Int32{}
}
