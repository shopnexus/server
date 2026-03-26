package accountbiz

import (
	"encoding/json"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// --- List ---

type ListNotificationParams struct {
	Account accountmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

// ListNotification returns paginated notifications for the authenticated account.
func (b *AccountHandler) ListNotification(ctx restate.Context, params ListNotificationParams) (sharedmodel.PaginateResult[accountdb.AccountNotification], error) {
	var zero sharedmodel.PaginateResult[accountdb.AccountNotification]
	params.PaginationParams = params.Constrain()

	rows, err := b.storage.Querier().ListNotificationByAccount(ctx, accountdb.ListNotificationByAccountParams{
		AccountID: params.Account.ID,
		Limit:     null.Int32From(params.Limit.Int32),
		Offset:    params.Offset(),
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(rows) > 0 {
		total.SetValid(rows[0].TotalCount)
	}

	return sharedmodel.PaginateResult[accountdb.AccountNotification]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data: lo.Map(rows, func(r accountdb.ListNotificationByAccountRow, _ int) accountdb.AccountNotification {
			return r.AccountNotification
		}),
	}, nil
}

// --- Count Unread ---

type CountUnreadParams struct {
	AccountID uuid.UUID
}

// CountUnread returns the number of unread notifications for the given account.
func (b *AccountHandler) CountUnread(ctx restate.Context, params CountUnreadParams) (int64, error) {
	return b.storage.Querier().CountUnreadByAccount(ctx, params.AccountID)
}

// --- Mark Read ---

type MarkReadParams struct {
	Account accountmodel.AuthenticatedAccount
	IDs     []int64 `validate:"required,min=1"`
}

// MarkRead marks the specified notification IDs as read.
func (b *AccountHandler) MarkRead(ctx restate.Context, params MarkReadParams) error {
	return b.storage.Querier().MarkNotificationRead(ctx, accountdb.MarkNotificationReadParams{
		ID:        params.IDs,
		AccountID: params.Account.ID,
	})
}

// --- Mark All Read ---

type MarkAllReadParams struct {
	AccountID uuid.UUID
}

// MarkAllRead marks all unread notifications as read for the given account.
func (b *AccountHandler) MarkAllRead(ctx restate.Context, params MarkAllReadParams) error {
	return b.storage.Querier().MarkAllNotificationRead(ctx, params.AccountID)
}

// --- Create ---

type CreateNotificationParams struct {
	AccountID uuid.UUID
	Type      string
	Channel   string
	Title     string
	Content   string
	Metadata  json.RawMessage
}

// CreateNotification creates a new notification for the given account.
func (b *AccountHandler) CreateNotification(ctx restate.Context, params CreateNotificationParams) (accountdb.AccountNotification, error) {
	return b.storage.Querier().CreateNotification(ctx, accountdb.CreateNotificationParams{
		AccountID: params.AccountID,
		Type:      params.Type,
		Channel:   params.Channel,
		Title:     params.Title,
		Content:   params.Content,
		Metadata:  params.Metadata,
	})
}
