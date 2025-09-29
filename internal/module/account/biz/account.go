package accountbiz

import (
	"context"
	"errors"
	"fmt"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"

	"shopnexus-remastered/internal/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/utils/pgutil"
)

type AccountBiz struct {
	storage   *pgutil.Storage
	promotion *promotionbiz.PromotionBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(storage *pgutil.Storage, promotionBiz *promotionbiz.PromotionBiz) *AccountBiz {
	return &AccountBiz{
		storage:   storage,
		promotion: promotionBiz,
	}
}

type FindAccountParams struct {
	ID       null.Int64
	Username null.String
	Email    null.String
	Phone    null.String
}

func (s *AccountBiz) FindAccount(ctx context.Context, params FindAccountParams) (db.AccountBase, error) {
	if !params.Username.Valid && !params.Email.Valid && !params.Phone.Valid && !params.ID.Valid {
		return db.AccountBase{}, fmt.Errorf("at least one of username, email, or phone must be provided")
	}

	account, err := s.storage.GetAccountBase(ctx, db.GetAccountBaseParams{
		ID:       pgutil.NullInt64ToPgInt8(params.ID),
		Phone:    pgutil.NullStringToPgText(params.Phone),
		Email:    pgutil.NullStringToPgText(params.Email),
		Username: pgutil.NullStringToPgText(params.Username),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AccountBase{}, authmodel.ErrAccountNotFound
		}
		return account, err
	}

	return account, nil
}

type CreateAccountParams struct {
	Type     db.AccountType
	Username null.String
	Phone    null.String
	Email    null.String
	Password null.String
}

func (s *AccountBiz) CreateAccount(ctx context.Context, params CreateAccountParams) (db.AccountBase, error) {
	var zero db.AccountBase

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// Create account base
	account, err := txStorage.CreateDefaultAccountBase(ctx, db.CreateDefaultAccountBaseParams{
		Type:     params.Type,
		Phone:    pgutil.NullStringToPgText(params.Phone),
		Email:    pgutil.NullStringToPgText(params.Email),
		Username: pgutil.NullStringToPgText(params.Username),
		Password: pgutil.NullStringToPgText(params.Password),
	})
	if err != nil {
		return zero, err
	}

	// Create empty profile
	if _, err := txStorage.CreateDefaultAccountProfile(ctx, db.CreateDefaultAccountProfileParams{
		ID: account.ID,
	}); err != nil {
		return zero, err
	}

	// Create empty customer/vendor additional profile
	switch account.Type {
	case db.AccountTypeCustomer:
		_, err = txStorage.CreateDefaultAccountCustomer(ctx, db.CreateDefaultAccountCustomerParams{
			ID: account.ID,
		})
	case db.AccountTypeVendor:
		_, err = txStorage.CreateDefaultAccountVendor(ctx, account.ID)
	default:
		return zero, fmt.Errorf("unsupported account type: %v", account.Type)
	}
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return account, nil
}

type UpdateAccountParams struct {
	ID       int64
	Status   *db.AccountStatus
	Username null.String
	Phone    null.String
	Email    null.String
	Password null.String
}

func (s *AccountBiz) UpdateAccount(ctx context.Context, params UpdateAccountParams) (db.AccountBase, error) {
	account, err := s.storage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:       params.ID,
		Status:   *pgutil.PtrBrandedToPgType(&db.NullAccountStatus{}, params.Status),
		Phone:    pgutil.NullStringToPgText(params.Phone),
		Email:    pgutil.NullStringToPgText(params.Email),
		Username: pgutil.NullStringToPgText(params.Username),
		Password: pgutil.NullStringToPgText(params.Password),
	})
	if err != nil {
		return db.AccountBase{}, err
	}

	return account, nil
}

type GetProfileParams struct {
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
		defaultAddressID null.Int64
		description      null.String
	)
	if account.Type == db.AccountTypeCustomer {
		customer, err := s.storage.GetAccountCustomer(ctx, pgutil.Int64ToPgInt8(params.AccountID))
		if err != nil {
			return zero, err
		}
		defaultAddressID = pgutil.PgInt8ToNullInt64(customer.DefaultAddressID)
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

		Gender:        null.NewValue(profile.Gender.AccountGender, profile.Gender.Valid),
		Name:          pgutil.PgTextToNullString(profile.Name),
		DateOfBirth:   profile.DateOfBirth.Time,
		AvatarRsID:    pgutil.PgInt8ToNullInt64(profile.AvatarRsID),
		EmailVerified: profile.EmailVerified,
		PhoneVerified: profile.PhoneVerified,

		DefaultAddressID: defaultAddressID,
		Description:      description,
	}, nil
}
