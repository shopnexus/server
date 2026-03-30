package accountbiz

import (
	"encoding/json"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
)

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
		return zero, sharedmodel.WrapErr("list notifications", err)
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

type CountUnreadParams struct {
	AccountID uuid.UUID
}

// CountUnread returns the number of unread notifications for the given account.
func (b *AccountHandler) CountUnread(ctx restate.Context, params CountUnreadParams) (int64, error) {
	count, err := b.storage.Querier().CountUnreadByAccount(ctx, params.AccountID)
	if err != nil {
		return 0, sharedmodel.WrapErr("count unread notifications", err)
	}
	return count, nil
}

type MarkReadParams struct {
	Account accountmodel.AuthenticatedAccount
	IDs     []int64 `validate:"required,min=1"`
}

// MarkRead marks the specified notification IDs as read.
func (b *AccountHandler) MarkRead(ctx restate.Context, params MarkReadParams) error {
	if err := b.storage.Querier().MarkNotificationRead(ctx, accountdb.MarkNotificationReadParams{
		ID:        params.IDs,
		AccountID: params.Account.ID,
	}); err != nil {
		return sharedmodel.WrapErr("mark notification read", err)
	}

	return nil
}

type MarkAllReadParams struct {
	AccountID uuid.UUID
}

// MarkAllRead marks all unread notifications as read for the given account.
func (b *AccountHandler) MarkAllRead(ctx restate.Context, params MarkAllReadParams) error {
	if err := b.storage.Querier().MarkAllNotificationRead(ctx, params.AccountID); err != nil {
		return sharedmodel.WrapErr("mark all notifications read", err)
	}

	return nil
}

type CreateNotificationParams struct {
	AccountID uuid.UUID
	Type      accountmodel.NotificationType
	Channel   accountmodel.NotificationChannel
	Title     string
	Content   string
	Metadata  json.RawMessage
}

// CreateNotification creates a new notification for the given account.
func (b *AccountHandler) CreateNotification(ctx restate.Context, params CreateNotificationParams) (accountdb.AccountNotification, error) {
	noti, err := b.storage.Querier().CreateDefaultNotification(ctx, accountdb.CreateDefaultNotificationParams{
		AccountID: params.AccountID,
		Type:      string(params.Type),
		Channel:   string(params.Channel),
		Title:     params.Title,
		Content:   params.Content,
		Metadata:  params.Metadata,
	})
	if err != nil {
		return accountdb.AccountNotification{}, sharedmodel.WrapErr("create notification", err)
	}

	// Push real-time notification to SSE clients
	restate.ServiceSend(ctx, "Common", "PushEvent").Send(commonbiz.PushEventParams{
		AccountID: params.AccountID,
		Type:      commonbiz.SSENotification,
		Data:      noti,
	})

	return noti, nil
}
