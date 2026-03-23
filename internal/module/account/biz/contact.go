package accountbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListContactParams struct {
	AccountID []uuid.UUID `validate:"dive,required"`
	ID        []uuid.UUID `validate:"omitempty,dive"`
}

// ListContact returns contacts matching the given account and contact IDs.
func (b *AccountBiz) ListContact(ctx restate.Context, params ListContactParams) ([]accountdb.AccountContact, error) {
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

// GetContact returns a single contact by ID for the authenticated account.
func (b *AccountBiz) GetContact(ctx restate.Context, params GetContactParams) (accountdb.AccountContact, error) {
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
		return zero, accountmodel.ErrContactNotFound
	}

	return result[0], nil
}

type CreateContactParams struct {
	Account     accountmodel.AuthenticatedAccount
	FullName    string                       `validate:"required"`
	Phone       string                       `validate:"required"`
	Address     string                       `validate:"required"`
	AddressType accountdb.AccountAddressType `validate:"required,validateFn=Valid"`
}

// CreateContact creates a new contact for the authenticated account.
func (b *AccountBiz) CreateContact(ctx restate.Context, params CreateContactParams) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbContact, err := b.storage.Querier().CreateDefaultContact(ctx, accountdb.CreateDefaultContactParams{
		AccountID:   params.Account.ID,
		FullName:    params.FullName,
		Phone:       params.Phone,
		Address:     params.Address,
		AddressType: params.AddressType,
	})
	if err != nil {
		return zero, fmt.Errorf("create contact: %w", err)
	}

	total, err := b.storage.Querier().CountContact(ctx, accountdb.CountContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
	if err != nil {
		return zero, fmt.Errorf("create contact: %w", err)
	}
	if total == 1 {
		if _, err := b.storage.Querier().UpdateProfile(ctx, accountdb.UpdateProfileParams{
			ID:               params.Account.ID,
			DefaultContactID: uuid.NullUUID{UUID: dbContact.ID, Valid: true},
		}); err != nil {
			return zero, fmt.Errorf("create contact: %w", err)
		}
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

// UpdateContact updates the specified contact fields.
func (b *AccountBiz) UpdateContact(ctx restate.Context, params UpdateContactParams) (accountdb.AccountContact, error) {
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

// DeleteContact removes a contact belonging to the authenticated account.
func (b *AccountBiz) DeleteContact(ctx restate.Context, params DeleteContactParams) error {
	return b.storage.Querier().DeleteContact(ctx, accountdb.DeleteContactParams{
		ID:        []uuid.UUID{params.ContactID},
		AccountID: []uuid.UUID{params.Account.ID},
	})
}

// GetDefaultContact returns the default contact for each of the given account IDs.
func (b *AccountBiz) GetDefaultContact(ctx restate.Context, accountIDs []uuid.UUID) (map[uuid.UUID]accountdb.AccountContact, error) {
	contacts, err := b.storage.Querier().ListDefaultContact(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	if len(contacts) != len(lo.Uniq(accountIDs)) {
		return nil, accountmodel.ErrNoDefaultContact
	}

	return lo.KeyBy(contacts, func(c accountdb.AccountContact) uuid.UUID { return c.AccountID }), nil
}
