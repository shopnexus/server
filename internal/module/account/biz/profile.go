package accountbiz

import (
	"context"
	"fmt"

	accountdb "shopnexus-remastered/internal/module/account/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListProfileParams struct {
	sharedmodel.PaginationParams
	Issuer     accountmodel.AuthenticatedAccount // Who is requesting the profiles
	AccountIDs []uuid.UUID                       `validate:"dive,required"`
}

func (b *AccountBiz) ListProfile(ctx context.Context, params ListProfileParams) (sharedmodel.PaginateResult[accountmodel.Profile], error) {
	var result sharedmodel.PaginateResult[accountmodel.Profile]
	if err := validator.Validate(params); err != nil {
		return result, err
	}

	listProfile, err := b.storage.Querier().ListCountProfile(ctx, accountdb.ListCountProfileParams{
		ID:     params.AccountIDs,
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
	if err != nil {
		return result, err
	}

	var total null.Int64
	if len(listProfile) > 0 {
		total.SetValid(listProfile[0].TotalCount)
	}

	dbProfiles := lo.Map(listProfile, func(row accountdb.ListCountProfileRow, _ int) accountdb.AccountProfile {
		return row.AccountProfile
	})

	listAccount, err := b.storage.Querier().ListAccount(ctx, accountdb.ListAccountParams{
		ID: lo.Map(params.AccountIDs, func(id uuid.UUID, _ int) uuid.UUID { return id }),
	})
	if err != nil {
		return result, err
	}

	accountMap := lo.KeyBy(listAccount, func(account accountdb.AccountAccount) uuid.UUID {
		return account.ID
	})

	profiles := make([]accountmodel.Profile, 0, len(dbProfiles))
	for _, dbProfile := range dbProfiles {
		account := accountMap[dbProfile.ID]

		profiles = append(profiles, accountmodel.Profile{
			ID:          account.ID,
			DateCreated: account.DateCreated,
			DateUpdated: account.DateUpdated,

			Type:     account.Type,
			Status:   account.Status,
			Phone:    account.Phone,
			Email:    account.Email,
			Username: account.Username,

			Gender:           null.NewValue(dbProfile.Gender.AccountGender, dbProfile.Gender.Valid),
			Name:             dbProfile.Name,
			DateOfBirth:      dbProfile.DateOfBirth,
			EmailVerified:    dbProfile.EmailVerified,
			PhoneVerified:    dbProfile.PhoneVerified,
			DefaultContactID: dbProfile.DefaultContactID,
			AvatarURL:        b.common.GetResourceURLByID(ctx, dbProfile.AvatarRsID.UUID),
		})
	}

	return sharedmodel.PaginateResult[accountmodel.Profile]{
		PageParams: params.PaginationParams,
		Data:       profiles,
		Total:      total,
	}, nil
}

type GetProfileParams struct {
	Issuer    accountmodel.AuthenticatedAccount // Who is requesting the profile
	AccountID uuid.UUID
}

func (b *AccountBiz) GetProfile(ctx context.Context, params GetProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile
	profile, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	account, err := b.storage.Querier().GetAccount(ctx, accountdb.GetAccountParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	var (
		description null.String
	)
	if account.Type == accountdb.AccountTypeCustomer {
		// customer, err := s.storage.GetCustomer(ctx, pgutil.Int64ToPgInt8(params.AccountID))
		// if err != nil {
		// 	return zero, err
		// }
	}
	if account.Type == accountdb.AccountTypeVendor {
		vendor, err := b.storage.Querier().GetVendor(ctx, uuid.NullUUID{UUID: params.AccountID, Valid: true})
		if err != nil {
			return zero, err
		}
		description.SetValid(vendor.Description)
	}

	return accountmodel.Profile{
		ID:          account.ID,
		DateCreated: account.DateCreated,
		DateUpdated: account.DateUpdated,

		Type:     account.Type,
		Status:   account.Status,
		Phone:    account.Phone,
		Email:    account.Email,
		Username: account.Username,

		Gender:           null.NewValue(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:             profile.Name,
		DateOfBirth:      profile.DateOfBirth,
		EmailVerified:    profile.EmailVerified,
		PhoneVerified:    profile.PhoneVerified,
		DefaultContactID: profile.DefaultContactID,
		AvatarURL:        b.common.GetResourceURLByID(ctx, profile.AvatarRsID.UUID),

		// Vendor fields
		Description: description,
	}, nil
}

type UpdateProfileParams struct {
	Storage   AccountStorage
	Issuer    accountmodel.AuthenticatedAccount // Who is performing the update
	AccountID uuid.UUID                         // Whose profile to be updated

	// Account base fields
	Status   accountdb.AccountStatus
	Username null.String
	Phone    null.String
	Email    null.String

	// Profile fields
	Gender           accountdb.AccountGender
	Name             null.String
	DateOfBirth      null.Time
	AvatarRsID       uuid.NullUUID
	DefaultContactID uuid.NullUUID

	// Vendor fields
	Description null.String
}

func (b *AccountBiz) UpdateProfile(ctx context.Context, params UpdateProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		account accountdb.AccountAccount
		profile accountdb.AccountProfile
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage AccountStorage) error {
		var err error

		account, err = txStorage.Querier().UpdateAccount(ctx, accountdb.UpdateAccountParams{
			ID:       params.AccountID,
			Status:   accountdb.NullAccountStatus{AccountStatus: params.Status, Valid: params.Status != ""},
			Username: params.Username,
			Phone:    params.Phone,
			Email:    params.Email,
		})
		if err != nil {
			return err
		}

		profile, err = txStorage.Querier().UpdateProfile(ctx, accountdb.UpdateProfileParams{
			ID:               params.AccountID,
			Gender:           accountdb.NullAccountGender{AccountGender: params.Gender, Valid: params.Gender != ""},
			Name:             params.Name,
			DateOfBirth:      params.DateOfBirth,
			AvatarRsID:       params.AvatarRsID,
			DefaultContactID: params.DefaultContactID,
		})
		if err != nil {
			return err
		}

		// Update customer/vendor additional profile
		switch account.Type {
		case accountdb.AccountTypeCustomer:
			_, err = txStorage.Querier().UpdateCustomer(ctx, accountdb.UpdateCustomerParams{
				ID: params.AccountID,
			})
		case accountdb.AccountTypeVendor:
			_, err = txStorage.Querier().UpdateVendor(ctx, accountdb.UpdateVendorParams{
				ID:          params.AccountID,
				Description: params.Description,
			})
		}
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update profile: %w", err)
	}

	return accountmodel.Profile{
		ID:          profile.ID,
		DateCreated: profile.DateCreated,
		DateUpdated: profile.DateUpdated,

		Type:     account.Type,
		Status:   account.Status,
		Phone:    account.Phone,
		Email:    account.Email,
		Username: account.Username,

		Gender:           null.NewValue(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:             profile.Name,
		DateOfBirth:      profile.DateOfBirth,
		EmailVerified:    profile.EmailVerified,
		PhoneVerified:    profile.PhoneVerified,
		DefaultContactID: profile.DefaultContactID,
		Description:      params.Description,
		AvatarURL:        b.common.GetResourceURLByID(ctx, profile.AvatarRsID.UUID),
	}, nil
}
