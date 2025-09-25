package catalogmodel

import (
	"github.com/jackc/pgx/v5/pgtype"

	sharedmodel "shopnexus-remastered/internal/module/shared/model"
)

type Comment struct {
	ID          int64                  `json:"id"`
	Account     CommentAccount         `json:"account"`
	Body        string                 `json:"body"`
	Upvote      int64                  `json:"upvote"`
	Downvote    int64                  `json:"downvote"`
	Score       int32                  `json:"score"`
	DateCreated pgtype.Timestamptz     `json:"date_created"`
	DateUpdated pgtype.Timestamptz     `json:"date_updated"`
	Resources   []sharedmodel.Resource `json:"resources"`
}

type CommentAccount struct {
	ID       int64                 `json:"id"`
	Name     string                `json:"name"`
	Verified bool                  `json:"verified"`
	Avatar   *sharedmodel.Resource `json:"avatar"`
}
