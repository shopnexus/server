package accountbiz

import (
	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// AccountBizService wraps AccountBiz for Restate registration.
// All methods accept restate.Context and delegate to AccountBiz with context.Context.
type AccountBizService struct {
	biz *AccountBiz
}

func NewAccountBizService(biz *AccountBiz) *AccountBizService {
	return &AccountBizService{biz: biz}
}

func (s *AccountBizService) Login(ctx restate.Context, params LoginParams) (LoginResult, error) {
	return s.biz.Login(ctx, params)
}

func (s *AccountBizService) Register(ctx restate.Context, params RegisterParams) (RegisterResult, error) {
	return s.biz.Register(ctx, params)
}

func (s *AccountBizService) Refresh(ctx restate.Context, refreshToken string) (RefreshResult, error) {
	return s.biz.Refresh(ctx, refreshToken)
}

func (s *AccountBizService) GetProfile(ctx restate.Context, params GetProfileParams) (accountmodel.Profile, error) {
	return s.biz.GetProfile(ctx, params)
}

func (s *AccountBizService) ListProfile(ctx restate.Context, params ListProfileParams) (sharedmodel.PaginateResult[accountmodel.Profile], error) {
	return s.biz.ListProfile(ctx, params)
}

func (s *AccountBizService) UpdateProfile(ctx restate.Context, params UpdateProfileParams) (accountmodel.Profile, error) {
	return s.biz.UpdateProfile(ctx, params)
}

func (s *AccountBizService) DeleteAccount(ctx restate.Context, params DeleteAccountParams) error {
	return s.biz.DeleteAccount(ctx, params)
}

func (s *AccountBizService) ListContact(ctx restate.Context, params ListContactParams) ([]accountdb.AccountContact, error) {
	return s.biz.ListContact(ctx, params)
}

func (s *AccountBizService) GetContact(ctx restate.Context, params GetContactParams) (accountdb.AccountContact, error) {
	return s.biz.GetContact(ctx, params)
}

func (s *AccountBizService) CreateContact(ctx restate.Context, params CreateContactParams) (accountdb.AccountContact, error) {
	return s.biz.CreateContact(ctx, params)
}

func (s *AccountBizService) UpdateContact(ctx restate.Context, params UpdateContactParams) (accountdb.AccountContact, error) {
	return s.biz.UpdateContact(ctx, params)
}

func (s *AccountBizService) DeleteContact(ctx restate.Context, params DeleteContactParams) error {
	return s.biz.DeleteContact(ctx, params)
}

func (s *AccountBizService) GetDefaultContact(ctx restate.Context, accountIDs []uuid.UUID) (map[uuid.UUID]accountdb.AccountContact, error) {
	return s.biz.GetDefaultContact(ctx, accountIDs)
}

func (s *AccountBizService) AddFavorite(ctx restate.Context, params AddFavoriteParams) (accountdb.AccountFavorite, error) {
	return s.biz.AddFavorite(ctx, params)
}

func (s *AccountBizService) RemoveFavorite(ctx restate.Context, params RemoveFavoriteParams) error {
	return s.biz.RemoveFavorite(ctx, params)
}

func (s *AccountBizService) ListFavorite(ctx restate.Context, params ListFavoriteParams) (sharedmodel.PaginateResult[accountdb.AccountFavorite], error) {
	return s.biz.ListFavorite(ctx, params)
}

func (s *AccountBizService) CreatePaymentMethod(ctx restate.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	return s.biz.CreatePaymentMethod(ctx, params)
}

func (s *AccountBizService) ListPaymentMethod(ctx restate.Context, params ListPaymentMethodParams) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error) {
	return s.biz.ListPaymentMethod(ctx, params)
}

func (s *AccountBizService) UpdatePaymentMethod(ctx restate.Context, params UpdatePaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	return s.biz.UpdatePaymentMethod(ctx, params)
}

func (s *AccountBizService) DeletePaymentMethod(ctx restate.Context, params DeletePaymentMethodParams) error {
	return s.biz.DeletePaymentMethod(ctx, params)
}

func (s *AccountBizService) SetDefaultPaymentMethod(ctx restate.Context, params SetDefaultPaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
	return s.biz.SetDefaultPaymentMethod(ctx, params)
}
