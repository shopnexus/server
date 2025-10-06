package accountbiz

import (
	"context"
	"errors"
	"fmt"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"

	"shopnexus-remastered/internal/db"
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

type DeleteAccountParams struct {
	AccountID int64
}

func (s *AccountBiz) DeleteAccount(ctx context.Context, params DeleteAccountParams) error {
	if _, err := s.storage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:     params.AccountID,
		Status: db.NullAccountStatus{AccountStatus: db.AccountStatusSuspended, Valid: true},
	}); err != nil {
		return err
	}
	return nil
}
