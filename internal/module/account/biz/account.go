package accountbiz

import (
	"context"
	"errors"
	"fmt"
	"shopnexus-remastered/internal/utils/pgutil"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"

	"github.com/jackc/pgx/v5"
)

type AccountBiz struct {
	storage      *pgutil.Storage
	promotionBiz *promotionbiz.PromotionBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(storage *pgutil.Storage, promotionBiz *promotionbiz.PromotionBiz) *AccountBiz {
	return &AccountBiz{
		storage:      storage,
		promotionBiz: promotionBiz,
	}
}

type FindAccountParams struct {
	ID       *int64
	Code     *string
	Username *string
	Email    *string
	Phone    *string
}

func (s *AccountBiz) FindAccount(ctx context.Context, params FindAccountParams) (db.AccountBase, error) {
	if params.Code == nil && params.Username == nil && params.Email == nil && params.Phone == nil && params.ID == nil {
		return db.AccountBase{}, fmt.Errorf("at least one of username, email, or phone must be provided")
	}

	account, err := s.storage.GetAccountBase(ctx, db.GetAccountBaseParams{
		ID:       pgutil.PtrToPgtype(params.ID, pgutil.Int64ToPgInt8),
		Username: pgutil.PtrToPgtype(params.Username, pgutil.StringToPgText),
		Email:    pgutil.PtrToPgtype(params.Email, pgutil.StringToPgText),
		Phone:    pgutil.PtrToPgtype(params.Phone, pgutil.StringToPgText),
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
	Username *string
	Phone    *string
	Email    *string
	Password *string
}

func (s *AccountBiz) CreateAccount(ctx context.Context, params CreateAccountParams) (db.AccountBase, error) {
	account, err := s.storage.CreateDefaultAccountBase(ctx, db.CreateDefaultAccountBaseParams{
		Type:     params.Type,
		Phone:    pgutil.PtrToPgtype(params.Phone, pgutil.StringToPgText),
		Email:    pgutil.PtrToPgtype(params.Email, pgutil.StringToPgText),
		Username: pgutil.PtrToPgtype(params.Username, pgutil.StringToPgText),
		Password: pgutil.PtrToPgtype(params.Password, pgutil.StringToPgText),
	})
	if err != nil {
		return db.AccountBase{}, err
	}

	return account, nil
}

type UpdateAccountParams struct {
	ID       int64
	Status   *db.AccountStatus
	Username *string
	Phone    *string
	Email    *string
	Password *string
}

func (s *AccountBiz) UpdateAccount(ctx context.Context, params UpdateAccountParams) (db.AccountBase, error) {
	account, err := s.storage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:       params.ID,
		Status:   *pgutil.PtrBrandedToPgType(&db.NullAccountStatus{}, params.Status),
		Phone:    pgutil.PtrToPgtype(params.Phone, pgutil.StringToPgText),
		Email:    pgutil.PtrToPgtype(params.Email, pgutil.StringToPgText),
		Username: pgutil.PtrToPgtype(params.Username, pgutil.StringToPgText),
		Password: pgutil.PtrToPgtype(params.Password, pgutil.StringToPgText),
	})
	if err != nil {
		return db.AccountBase{}, err
	}

	return account, nil
}
