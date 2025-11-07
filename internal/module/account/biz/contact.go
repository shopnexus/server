package accountbiz

import (
	"context"
	"fmt"
	"time"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListContactParams struct {
	Account authmodel.AuthenticatedAccount
}

func (b *AccountBiz) ListContact(ctx context.Context, params ListContactParams) ([]db.AccountContact, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	contacts, err := b.storage.ListAccountContact(ctx, db.ListAccountContactParams{
		AccountID: []int64{params.Account.ID},
	})
	if err != nil {
		return nil, err
	}

	return contacts, nil
}

type GetContactParams struct {
	Account   authmodel.AuthenticatedAccount
	ContactID int64 `validate:"required"`
}

func (b *AccountBiz) GetContact(ctx context.Context, params GetContactParams) (db.AccountContact, error) {
	var zero db.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	contact, err := b.storage.GetAccountContact(ctx, pgutil.Int64ToPgInt8(params.ContactID))
	if err != nil {
		return zero, err
	}

	return contact, nil
}

type CreateContactParams struct {
	Storage     pgsqlc.Storage
	Account     authmodel.AuthenticatedAccount
	FullName    string                `validate:"required"`
	Phone       string                `validate:"required"`
	Address     string                `validate:"required"`
	AddressType db.AccountAddressType `validate:"required,validateFn=Valid"`
}

func (b *AccountBiz) CreateContact(ctx context.Context, params CreateContactParams) (db.AccountContact, error) {
	var zero db.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var dbContact db.AccountContact

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		dbContact, err = txStorage.CreateDefaultAccountContact(ctx, db.CreateDefaultAccountContactParams{
			AccountID:   params.Account.ID,
			FullName:    params.FullName,
			Phone:       params.Phone,
			Address:     params.Address,
			AddressType: params.AddressType,
		})
		if err != nil {
			return err
		}

		total, err := txStorage.CountAccountContact(ctx, db.CountAccountContactParams{
			AccountID: []int64{params.Account.ID},
		})
		if err != nil {
			return err
		}
		if total == 1 {
			if _, err := txStorage.UpdateAccountProfile(ctx, db.UpdateAccountProfileParams{
				ID:               params.Account.ID,
				DefaultContactID: pgutil.Int64ToPgInt8(dbContact.ID),
			}); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create contact: %w", err)
	}

	return dbContact, nil
}

type UpdateContactParams struct {
	Account     authmodel.AuthenticatedAccount
	ContactID   int64                 `validate:"required"`
	FullName    null.String           `validate:"omitnil"`
	Phone       null.String           `validate:"omitnil"`
	Address     null.String           `validate:"omitnil"`
	AddressType db.AccountAddressType `validate:"omitempty,validateFn=Valid"`

	PhoneVerified null.Bool `validate:"omitnil"`
}

func (b *AccountBiz) UpdateContact(ctx context.Context, params UpdateContactParams) (db.AccountContact, error) {
	var zero db.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	updatedContact, err := b.storage.UpdateAccountContact(ctx, db.UpdateAccountContactParams{
		ID:          params.ContactID,
		FullName:    pgutil.NullStringToPgText(params.FullName),
		Phone:       pgutil.NullStringToPgText(params.Phone),
		Address:     pgutil.NullStringToPgText(params.Address),
		AddressType: db.NullAccountAddressType{AccountAddressType: params.AddressType, Valid: params.AddressType.Valid()},

		PhoneVerified: pgutil.NullBoolToPgBool(params.PhoneVerified),
		DateUpdated:   pgutil.TimeToPgTimestamptz(time.Now()),
	})
	if err != nil {
		return zero, err
	}

	return updatedContact, nil
}

type DeleteContactParams struct {
	Account   authmodel.AuthenticatedAccount
	ContactID int64
}

func (b *AccountBiz) DeleteContact(ctx context.Context, params DeleteContactParams) error {
	return b.storage.DeleteAccountContact(ctx, db.DeleteAccountContactParams{
		ID:        []int64{params.ContactID},
		AccountID: []int64{params.Account.ID},
	})
}
