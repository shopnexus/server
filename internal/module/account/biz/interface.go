package accountbiz

import (
	"context"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pubsub"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// AccountClient is the client interface for AccountBiz, which is used by other modules to call AccountBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface AccountClient -service AccountBiz
type AccountClient interface {
	// Auth
	Login(ctx context.Context, params LoginParams) (LoginResult, error)
	Register(ctx context.Context, params RegisterParams) (RegisterResult, error)
	Refresh(ctx context.Context, refreshToken string) (RefreshResult, error)

	// Profile
	GetProfile(ctx context.Context, params GetProfileParams) (accountmodel.Profile, error)
	ListProfile(ctx context.Context, params ListProfileParams) (sharedmodel.PaginateResult[accountmodel.Profile], error)
	UpdateProfile(ctx context.Context, params UpdateProfileParams) (accountmodel.Profile, error)

	// Account
	DeleteAccount(ctx context.Context, params DeleteAccountParams) error

	// Contact
	ListContact(ctx context.Context, params ListContactParams) ([]accountdb.AccountContact, error)
	GetContact(ctx context.Context, params GetContactParams) (accountdb.AccountContact, error)
	CreateContact(ctx context.Context, params CreateContactParams) (accountdb.AccountContact, error)
	UpdateContact(ctx context.Context, params UpdateContactParams) (accountdb.AccountContact, error)
	DeleteContact(ctx context.Context, params DeleteContactParams) error
	GetDefaultContact(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]accountdb.AccountContact, error)

	// Favorite
	AddFavorite(ctx context.Context, params AddFavoriteParams) (accountdb.AccountFavorite, error)
	RemoveFavorite(ctx context.Context, params RemoveFavoriteParams) error
	ListFavorite(ctx context.Context, params ListFavoriteParams) (sharedmodel.PaginateResult[accountdb.AccountFavorite], error)
	// Payment Method
	CreatePaymentMethod(ctx context.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error)
	ListPaymentMethod(ctx context.Context, params ListPaymentMethodParams) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error)
	UpdatePaymentMethod(ctx context.Context, params UpdatePaymentMethodParams) (accountdb.AccountPaymentMethod, error)
	DeletePaymentMethod(ctx context.Context, params DeletePaymentMethodParams) error
	SetDefaultPaymentMethod(ctx context.Context, params SetDefaultPaymentMethodParams) (accountdb.AccountPaymentMethod, error)
}

type AccountStorage = pgsqlc.Storage[*accountdb.Queries]

type AccountBiz struct {
	tokenDuration        time.Duration
	jwtSecret            []byte
	refreshTokenDuration time.Duration
	refreshSecret        []byte

	storage AccountStorage
	pubsub  pubsub.Client
	common  *commonbiz.CommonBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(
	config *config.Config,
	storage AccountStorage,
	pubsub pubsub.Client,
	common *commonbiz.CommonBiz,
) *AccountBiz {
	return &AccountBiz{
		tokenDuration:        time.Duration(config.App.JWT.AccessTokenDuration * int64(time.Second)),
		jwtSecret:            []byte(config.App.JWT.Secret),
		refreshTokenDuration: time.Duration(config.App.JWT.RefreshTokenDuration * int64(time.Second)),
		refreshSecret:        []byte(config.App.JWT.RefreshSecret),

		storage: storage,
		pubsub:  pubsub,
		common:  common,
	}
}
