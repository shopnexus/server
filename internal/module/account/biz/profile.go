package accountbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
)

type GetProfileParams struct {
	Issuer    authmodel.AuthenticatedAccount // Who is requesting the profile
	AccountID int64
}

func (s *AccountBiz) GetProfile(ctx context.Context, params GetProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile
	profile, err := s.storage.GetAccountProfile(ctx, db.GetAccountProfileParams{
		ID: pgutil.Int64ToPgInt8(params.AccountID),
	})
	if err != nil {
		return zero, err
	}

	account, err := s.storage.GetAccountBase(ctx, db.GetAccountBaseParams{
		ID: pgutil.Int64ToPgInt8(params.AccountID),
	})
	if err != nil {
		return zero, err
	}

	var (
		description null.String
	)
	if account.Type == db.AccountTypeCustomer {
		// customer, err := s.storage.GetAccountCustomer(ctx, pgutil.Int64ToPgInt8(params.AccountID))
		// if err != nil {
		// 	return zero, err
		// }
	}
	if account.Type == db.AccountTypeVendor {
		vendor, err := s.storage.GetAccountVendor(ctx, pgutil.Int64ToPgInt8(params.AccountID))
		if err != nil {
			return zero, err
		}
		description.SetValid(vendor.Description)
	}

	return accountmodel.Profile{
		ID:          account.ID,
		DateCreated: account.DateCreated.Time,
		DateUpdated: account.DateUpdated.Time,

		Type:     account.Type,
		Status:   account.Status,
		Phone:    pgutil.PgTextToNullString(account.Phone),
		Email:    pgutil.PgTextToNullString(account.Email),
		Username: pgutil.PgTextToNullString(account.Username),

		Gender:           null.NewValue(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:             pgutil.PgTextToNullString(profile.Name),
		DateOfBirth:      profile.DateOfBirth.Time,
		EmailVerified:    profile.EmailVerified,
		PhoneVerified:    profile.PhoneVerified,
		DefaultContactID: pgutil.PgInt8ToNullInt64(profile.DefaultContactID),
		AvatarURL:        s.shared.GetResourceURLByID(ctx, profile.AvatarRsID.Int64),

		// Vendor fields
		Description: description,
	}, nil
}

type UpdateProfileParams struct {
	Issuer    authmodel.AuthenticatedAccount // Who is performing the update
	AccountID int64                          // Whose profile to be updated

	// Account base fields
	Status   db.AccountStatus
	Username null.String
	Phone    null.String
	Email    null.String

	// Profile fields
	Gender           db.AccountGender
	Name             null.String
	DateOfBirth      null.Time
	AvatarRsID       null.Int64
	DefaultContactID null.Int64

	// Vendor fields
	Description null.String
}

func (s *AccountBiz) UpdateProfile(ctx context.Context, params UpdateProfileParams) (accountmodel.Profile, error) {
	var zero accountmodel.Profile

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	account, err := txStorage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:       params.AccountID,
		Status:   db.NullAccountStatus{AccountStatus: params.Status, Valid: params.Status != ""},
		Username: pgutil.NullStringToPgText(params.Username),
		Phone:    pgutil.NullStringToPgText(params.Phone),
		Email:    pgutil.NullStringToPgText(params.Email),
	})
	if err != nil {
		return zero, err
	}

	profile, err := txStorage.UpdateAccountProfile(ctx, db.UpdateAccountProfileParams{
		ID:               params.AccountID,
		Gender:           db.NullAccountGender{AccountGender: params.Gender, Valid: params.Gender != ""},
		Name:             pgutil.NullStringToPgText(params.Name),
		DateOfBirth:      pgtype.Date{Time: params.DateOfBirth.Time, Valid: params.DateOfBirth.Valid},
		AvatarRsID:       pgutil.NullInt64ToPgInt8(params.AvatarRsID),
		DefaultContactID: pgutil.NullInt64ToPgInt8(params.DefaultContactID),
	})
	if err != nil {
		return zero, err
	}

	// Update customer/vendor additional profile
	switch account.Type {
	case db.AccountTypeCustomer:
		_, err = txStorage.UpdateAccountCustomer(ctx, db.UpdateAccountCustomerParams{
			ID: params.AccountID,
		})
	case db.AccountTypeVendor:
		_, err = txStorage.UpdateAccountVendor(ctx, db.UpdateAccountVendorParams{
			ID:          params.AccountID,
			Description: pgutil.NullStringToPgText(params.Description),
		})
	}
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return accountmodel.Profile{
		ID:          profile.ID,
		DateCreated: profile.DateCreated.Time,
		DateUpdated: profile.DateUpdated.Time,

		Type:     account.Type,
		Status:   account.Status,
		Phone:    pgutil.PgTextToNullString(account.Phone),
		Email:    pgutil.PgTextToNullString(account.Email),
		Username: pgutil.PgTextToNullString(account.Username),

		Gender:           null.NewValue(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:             pgutil.PgTextToNullString(profile.Name),
		DateOfBirth:      profile.DateOfBirth.Time,
		EmailVerified:    profile.EmailVerified,
		PhoneVerified:    profile.PhoneVerified,
		DefaultContactID: pgutil.PgInt8ToNullInt64(profile.DefaultContactID),
		Description:      params.Description,
		AvatarURL:        s.shared.GetResourceURLByID(ctx, profile.AvatarRsID.Int64),
	}, nil
}
