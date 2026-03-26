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
func (b *AccountHandler) ListContact(ctx restate.Context, params ListContactParams) ([]accountdb.AccountContact, error) {
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
func (b *AccountHandler) GetContact(ctx restate.Context, params GetContactParams) (accountdb.AccountContact, error) {
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
func (b *AccountHandler) CreateContact(ctx restate.Context, params CreateContactParams) (accountdb.AccountContact, error) {
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
		Latitude:    params.Latitude,
		Longitude:   params.Longitude,
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
	Latitude    null.Float                   `validate:"omitnil"`
	Longitude   null.Float                   `validate:"omitnil"`

	PhoneVerified null.Bool `validate:"omitnil"`
}

// UpdateContact updates the specified contact fields.
func (b *AccountHandler) UpdateContact(ctx restate.Context, params UpdateContactParams) (accountdb.AccountContact, error) {
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
		Latitude:    params.Latitude,
		Longitude:   params.Longitude,

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
// Cannot delete the last remaining contact. If the default contact is deleted,
// the most recently created remaining contact becomes the new default.
func (b *AccountHandler) DeleteContact(ctx restate.Context, params DeleteContactParams) error {
	total, err := b.storage.Querier().CountContact(ctx, accountdb.CountContactParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
	if err != nil {
		return err
	}
	if total <= 1 {
		return accountmodel.ErrCannotDeleteLastContact.Terminal()
	}

	// Check if we're deleting the default contact
	profile, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{ID: uuid.NullUUID{UUID: params.Account.ID, Valid: true}})
	isDefault := err == nil && profile.DefaultContactID.Valid && profile.DefaultContactID.UUID == params.ContactID

	// Delete the contact
	if err := b.storage.Querier().DeleteContact(ctx, accountdb.DeleteContactParams{
		ID:        []uuid.UUID{params.ContactID},
		AccountID: []uuid.UUID{params.Account.ID},
	}); err != nil {
		return err
	}

	// If we deleted the default, reassign to the most recent remaining contact
	if isDefault {
		remaining, err := b.storage.Querier().ListContact(ctx, accountdb.ListContactParams{
			AccountID: []uuid.UUID{params.Account.ID},
		})
		if err == nil && len(remaining) > 0 {
			b.storage.Querier().UpdateProfile(ctx, accountdb.UpdateProfileParams{
				ID:               params.Account.ID,
				DefaultContactID: uuid.NullUUID{UUID: remaining[0].ID, Valid: true},
			})
		}
	}

	return nil
}

// GetDefaultContact returns the default contact for each of the given account IDs.
func (b *AccountHandler) GetDefaultContact(ctx restate.Context, accountIDs []uuid.UUID) (map[uuid.UUID]accountdb.AccountContact, error) {
	contacts, err := b.storage.Querier().ListDefaultContact(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	if len(contacts) != len(lo.Uniq(accountIDs)) {
		return nil, accountmodel.ErrNoDefaultContact.Terminal()
	}

	return lo.KeyBy(contacts, func(c accountdb.AccountContact) uuid.UUID { return c.AccountID }), nil
}
