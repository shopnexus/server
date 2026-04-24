package accountbiz

import (
	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	sharedmodel "shopnexus-server/internal/shared/model"
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
func (b *AccountHandler) ListContact(
	ctx restate.Context,
	params ListContactParams,
) ([]accountdb.AccountContact, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list contact", err)
	}

	contacts, err := b.storage.Querier().ListContact(ctx, accountdb.ListContactParams{
		AccountID: params.AccountID,
		ID:        params.ID,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db list contact", err)
	}

	return contacts, nil
}

type GetContactParams struct {
	Account   accountmodel.AuthenticatedAccount
	ContactID uuid.UUID `validate:"required"`
}

// GetContact returns a single contact by ID for the authenticated account.
func (b *AccountHandler) GetContact(ctx restate.Context, params GetContactParams) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate get contact", err)
	}

	result, err := b.ListContact(ctx, ListContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
		ID:        []uuid.UUID{params.ContactID},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get contact", err)
	}

	if len(result) == 0 {
		return zero, accountmodel.ErrContactNotFound.Terminal()
	}

	return result[0], nil
}

type CreateContactParams struct {
	Account     accountmodel.AuthenticatedAccount
	FullName    string                       `validate:"required"`
	Phone       string                       `validate:"required"`
	Address     string                       `validate:"required"`
	AddressType accountdb.AccountAddressType `validate:"required,validateFn=Valid"`
	Latitude    null.Float                   `validate:"omitnil"`
	Longitude   null.Float                   `validate:"omitnil"`
}

// CreateContact creates a new contact for the authenticated account.
func (b *AccountHandler) CreateContact(
	ctx restate.Context,
	params CreateContactParams,
) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create contact", err)
	}

	if err := b.assertAddressMatchesProfileCountry(ctx, params.Account.ID, params.Address); err != nil {
		return zero, err
	}

	dbContact, err := b.storage.Querier().CreateDefaultContact(ctx, accountdb.CreateDefaultContactParams{
		AccountID:   params.Account.ID,
		FullName:    params.FullName,
		Phone:       params.Phone,
		Address:     params.Address,
		AddressType: params.AddressType,
		Latitude:    params.Latitude.Float64,
		Longitude:   params.Longitude.Float64,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db create contact", err)
	}

	total, err := b.storage.Querier().CountContact(ctx, accountdb.CountContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db create contact", err)
	}
	if total == 1 {
		if err := b.storage.Querier().SetAccountDefaultContact(ctx, accountdb.SetAccountDefaultContactParams{
			ID:               params.Account.ID,
			DefaultContactID: uuid.NullUUID{UUID: dbContact.ID, Valid: true},
		}); err != nil {
			return zero, sharedmodel.WrapErr("set default contact", err)
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
	Latitude    null.Float                   `validate:"omitnil"`
	Longitude   null.Float                   `validate:"omitnil"`

	PhoneVerified null.Bool `validate:"omitnil"`
}

// UpdateContact updates the specified contact fields.
func (b *AccountHandler) UpdateContact(
	ctx restate.Context,
	params UpdateContactParams,
) (accountdb.AccountContact, error) {
	var zero accountdb.AccountContact

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate update contact", err)
	}

	if params.Address.Valid && params.Address.String != "" {
		if err := b.assertAddressMatchesProfileCountry(ctx, params.Account.ID, params.Address.String); err != nil {
			return zero, err
		}
	}

	updatedContact, err := b.storage.Querier().UpdateContact(ctx, accountdb.UpdateContactParams{
		ID:       params.ContactID,
		FullName: params.FullName,
		Phone:    params.Phone,
		Address:  params.Address,
		AddressType: accountdb.NullAccountAddressType{
			AccountAddressType: params.AddressType,
			Valid:              params.AddressType.Valid(),
		},
		Latitude:  params.Latitude,
		Longitude: params.Longitude,

		PhoneVerified: params.PhoneVerified,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db update contact", err)
	}

	return updatedContact, nil
}

type DeleteContactParams struct {
	Account   accountmodel.AuthenticatedAccount
	ContactID uuid.UUID
}

// DeleteContact removes a contact belonging to the authenticated account.
// Cannot delete the last remaining contact. If the default contact is deleted,
// the most recently created remaining contact becomes the new default.
func (b *AccountHandler) DeleteContact(ctx restate.Context, params DeleteContactParams) error {
	total, err := b.storage.Querier().CountContact(ctx, accountdb.CountContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
	if err != nil {
		return sharedmodel.WrapErr("db count contact", err)
	}
	if total <= 1 {
		return accountmodel.ErrCannotDeleteLastContact.Terminal()
	}

	// Check if we're deleting the default contact
	defaults, err := b.storage.Querier().GetAccountDefaults(ctx, params.Account.ID)
	isDefault := err == nil && defaults.DefaultContactID.Valid && defaults.DefaultContactID.UUID == params.ContactID

	// Delete the contact
	if err := b.storage.Querier().DeleteContact(ctx, accountdb.DeleteContactParams{
		ID:        []uuid.UUID{params.ContactID},
		AccountID: []uuid.UUID{params.Account.ID},
	}); err != nil {
		return sharedmodel.WrapErr("db delete contact", err)
	}

	// If we deleted the default, reassign to the most recent remaining contact
	if isDefault {
		remaining, err := b.storage.Querier().ListContact(ctx, accountdb.ListContactParams{
			AccountID: []uuid.UUID{params.Account.ID},
		})
		if err == nil && len(remaining) > 0 {
			_ = b.storage.Querier().SetAccountDefaultContact(ctx, accountdb.SetAccountDefaultContactParams{
				ID:               params.Account.ID,
				DefaultContactID: uuid.NullUUID{UUID: remaining[0].ID, Valid: true},
			})
		}
	}

	return nil
}

// assertAddressMatchesProfileCountry geocodes the given address via the
// common module and rejects the request if the resolved country does not
// match the owner's profile country. Used by CreateContact/UpdateContact so
// a user can only register addresses that resolve to their own country.
func (b *AccountHandler) assertAddressMatchesProfileCountry(
	ctx restate.Context,
	accountID uuid.UUID,
	address string,
) error {
	profile, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{
		ID: uuid.NullUUID{UUID: accountID, Valid: true},
	})
	if err != nil {
		return sharedmodel.WrapErr("load profile for address check", err)
	}

	resolvedCountry, err := b.common.ResolveCountry(ctx, address)
	if err != nil {
		return err
	}

	if resolvedCountry != profile.Country {
		return accountmodel.ErrContactAddressCountryMismatch.Fmt(resolvedCountry, profile.Country).Terminal()
	}
	return nil
}

// GetDefaultContact returns the default contact for each of the given account IDs.
func (b *AccountHandler) GetDefaultContact(
	ctx restate.Context,
	accountIDs []uuid.UUID,
) (map[uuid.UUID]accountdb.AccountContact, error) {
	contacts, err := b.storage.Querier().ListDefaultContact(ctx, accountIDs)
	if err != nil {
		return nil, sharedmodel.WrapErr("db list default contact", err)
	}
	if len(contacts) != len(lo.Uniq(accountIDs)) {
		return nil, accountmodel.ErrNoDefaultContact.Terminal()
	}

	return lo.KeyBy(contacts, func(c accountdb.AccountContact) uuid.UUID { return c.AccountID }), nil
}
