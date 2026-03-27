package accountbiz

import (
	"encoding/json"

	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type CreatePaymentMethodParams struct {
	Account   accountmodel.AuthenticatedAccount
	Type      string          `validate:"required"`
	Label     string          `validate:"required"`
	Data      json.RawMessage `validate:"required"`
	IsDefault bool
}

// CreatePaymentMethod creates a new payment method for the authenticated account.
func (b *AccountHandler) CreatePaymentMethod(ctx restate.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	var zero accountdb.AccountPaymentMethod

	result, err := b.storage.Querier().CreateDefaultPaymentMethod(ctx, accountdb.CreateDefaultPaymentMethodParams{
		AccountID: params.Account.ID,
		Type:      params.Type,
		Label:     params.Label,
		Data:      params.Data,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create payment method", err)
	}

	if params.IsDefault {
		if err := b.storage.Querier().UnsetDefaultPaymentMethod(ctx, params.Account.ID); err != nil {
			return zero, sharedmodel.WrapErr("create payment method", err)
		}
		result, err = b.storage.Querier().SetDefaultPaymentMethod(ctx, accountdb.SetDefaultPaymentMethodParams{
			ID:        result.ID,
			AccountID: params.Account.ID,
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create payment method", err)
		}
	}

	return result, nil
}

type ListPaymentMethodParams struct {
	Account accountmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

// ListPaymentMethod returns a paginated list of payment methods for the account.
func (b *AccountHandler) ListPaymentMethod(ctx restate.Context, params ListPaymentMethodParams) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error) {
	var zero sharedmodel.PaginateResult[accountdb.AccountPaymentMethod]
	params.PaginationParams = params.Constrain()

	rows, err := b.storage.Querier().ListCountPaymentMethod(ctx, accountdb.ListCountPaymentMethodParams{
		AccountID: []uuid.UUID{params.Account.ID},
		Limit:     null.Int32From(params.Limit.Int32),
		Offset:    params.Offset(),
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list payment methods", err)
	}

	methods := make([]accountdb.AccountPaymentMethod, len(rows))
	var total int64
	for i, row := range rows {
		methods[i] = row.AccountPaymentMethod
		total = row.TotalCount
	}

	return sharedmodel.PaginateResult[accountdb.AccountPaymentMethod]{
		PageParams: params.PaginationParams,
		Data:       methods,
		Total:      null.IntFrom(total),
	}, nil
}

type UpdatePaymentMethodParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID       `validate:"required"`
	Type    null.String     `validate:"omitnil"`
	Label   null.String     `validate:"omitnil"`
	Data    json.RawMessage `validate:"omitempty"`
}

// UpdatePaymentMethod updates the specified payment method fields.
func (b *AccountHandler) UpdatePaymentMethod(ctx restate.Context, params UpdatePaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	var zero accountdb.AccountPaymentMethod

	result, err := b.storage.Querier().UpdatePaymentMethod(ctx, accountdb.UpdatePaymentMethodParams{
		ID:        params.ID,
		AccountID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
		Type:      params.Type,
		Label:     params.Label,
		Data:      params.Data,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update payment method", err)
	}

	return result, nil
}

type DeletePaymentMethodParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

// DeletePaymentMethod removes a payment method belonging to the authenticated account.
func (b *AccountHandler) DeletePaymentMethod(ctx restate.Context, params DeletePaymentMethodParams) error {
	if err := b.storage.Querier().DeletePaymentMethod(ctx, accountdb.DeletePaymentMethodParams{
		ID:        []uuid.UUID{params.ID},
		AccountID: []uuid.UUID{params.Account.ID},
	}); err != nil {
		return sharedmodel.WrapErr("delete payment method", err)
	}

	return nil
}

type SetDefaultPaymentMethodParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

// SetDefaultPaymentMethod marks the given payment method as the account's default.
func (b *AccountHandler) SetDefaultPaymentMethod(ctx restate.Context, params SetDefaultPaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	var zero accountdb.AccountPaymentMethod

	if err := b.storage.Querier().UnsetDefaultPaymentMethod(ctx, params.Account.ID); err != nil {
		return zero, sharedmodel.WrapErr("set default payment method", err)
	}

	result, err := b.storage.Querier().SetDefaultPaymentMethod(ctx, accountdb.SetDefaultPaymentMethodParams{
		ID:        params.ID,
		AccountID: params.Account.ID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("set default payment method", err)
	}

	return result, nil
}
