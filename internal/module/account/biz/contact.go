package accountbiz

import (
	"context"
	"fmt"

	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListContactParams struct {
	AccountID []uuid.UUID `validate:"dive,required"`
	ID        []uuid.UUID `validate:"omitempty,dive"`
}

func (b *AccountBiz) ListContact(ctx context.Context, params ListContactParams) ([]accountdb.AccountContact, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	contacts, err := b.storage.Querier().ListContact(ctx, accountdb.ListContactParams{
		AccountID: params.AccountID,
		ID:        params.ID,
	})
	if err != nil {
		return nil, err
	}

	return contacts, nil
}

type GetContactParams struct {
	Account   accountmodel.AuthenticatedAccount
	ContactID uuid.UUID `validate:"required"`
}

func (b *AccountBiz) GetContact(ctx context.Context, params GetContactParams) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	result, err := b.ListContact(ctx, ListContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
		ID:        []uuid.UUID{params.ContactID},
	})
	if err != nil {
		return zero, err
	}

	if len(result) == 0 {
		return zero, fmt.Errorf("contact not found")
	}

	return result[0], nil
}

type CreateContactParams struct {
	Storage     AccountStorage
	Account     accountmodel.AuthenticatedAccount
	FullName    string                       `validate:"required"`
	Phone       string                       `validate:"required"`
	Address     string                       `validate:"required"`
	AddressType accountdb.AccountAddressType `validate:"required,validateFn=Valid"`
}

func (b *AccountBiz) CreateContact(ctx context.Context, params CreateContactParams) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var dbContact accountdb.AccountContact

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage AccountStorage) error {
		var err error
		dbContact, err = txStorage.Querier().CreateDefaultContact(ctx, accountdb.CreateDefaultContactParams{
			AccountID:   params.Account.ID,
			FullName:    params.FullName,
			Phone:       params.Phone,
			Address:     params.Address,
			AddressType: params.AddressType,
		})
		if err != nil {
			return err
		}

		total, err := txStorage.Querier().CountContact(ctx, accountdb.CountContactParams{
			AccountID: []uuid.UUID{params.Account.ID},
		})
		if err != nil {
			return err
		}
		if total == 1 {
			if _, err := txStorage.Querier().UpdateProfile(ctx, accountdb.UpdateProfileParams{
				ID:               params.Account.ID,
				DefaultContactID: uuid.NullUUID{UUID: dbContact.ID, Valid: true},
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
	Account     accountmodel.AuthenticatedAccount
	ContactID   uuid.UUID                    `validate:"required"`
	FullName    null.String                  `validate:"omitnil"`
	Phone       null.String                  `validate:"omitnil"`
	Address     null.String                  `validate:"omitnil"`
	AddressType accountdb.AccountAddressType `validate:"omitempty,validateFn=Valid"`

	PhoneVerified null.Bool `validate:"omitnil"`
}

func (b *AccountBiz) UpdateContact(ctx context.Context, params UpdateContactParams) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	updatedContact, err := b.storage.Querier().UpdateContact(ctx, accountdb.UpdateContactParams{
		ID:          params.ContactID,
		FullName:    params.FullName,
		Phone:       params.Phone,
		Address:     params.Address,
		AddressType: accountdb.NullAccountAddressType{AccountAddressType: params.AddressType, Valid: params.AddressType.Valid()},

		PhoneVerified: params.PhoneVerified,
	})
	if err != nil {
		return zero, err
	}

	return updatedContact, nil
}

type DeleteContactParams struct {
	Account   accountmodel.AuthenticatedAccount
	ContactID uuid.UUID
}

func (b *AccountBiz) DeleteContact(ctx context.Context, params DeleteContactParams) error {
	return b.storage.Querier().DeleteContact(ctx, accountdb.DeleteContactParams{
		ID:        []uuid.UUID{params.ContactID},
		AccountID: []uuid.UUID{params.Account.ID},
	})
}

func (b *AccountBiz) GetDefaultContact(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]accountdb.AccountContact, error) {
	contacts, err := b.storage.Querier().ListDefaultContact(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	if len(contacts) != len(accountIDs) {
		return nil, fmt.Errorf("some accounts have no default contact")
	}

	return lo.KeyBy(contacts, func(c accountdb.AccountContact) uuid.UUID { return c.AccountID }), nil
}
