package accountbiz

import (
	"context"
	"time"

	"shopnexus-server/config"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// AccountBiz is the client interface for AccountBizHandler, which is used by other modules to call AccountBizHandler methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface AccountBiz -service Account
type AccountBiz interface {
	// Auth
	Login(ctx context.Context, params LoginParams) (LoginResult, error)
	Register(ctx context.Context, params RegisterParams) (RegisterResult, error)
	Refresh(ctx context.Context, refreshToken string) (RefreshResult, error)

	// Profile
	GetProfile(ctx context.Context, params GetProfileParams) (accountmodel.Profile, error)
	ListProfile(ctx context.Context, params ListProfileParams) (sharedmodel.PaginateResult[accountmodel.Profile], error)
	UpdateProfile(ctx context.Context, params UpdateProfileParams) (accountmodel.Profile, error)
	UpdateProfileSettings(ctx context.Context, params UpdateProfileSettingsParams) (accountmodel.ProfileSettings, error)

	// Account
	SuspendAccount(ctx context.Context, params SuspendAccountParams) error

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
	ListFavorite(
		ctx context.Context,
		params ListFavoriteParams,
	) (sharedmodel.PaginateResult[accountdb.AccountFavorite], error)
	CheckFavorites(ctx context.Context, params CheckFavoritesParams) (map[uuid.UUID]bool, error)

	// Notification
	ListNotification(
		ctx context.Context,
		params ListNotificationParams,
	) (sharedmodel.PaginateResult[accountdb.AccountNotification], error)
	CountUnread(ctx context.Context, params CountUnreadParams) (int64, error)
	MarkRead(ctx context.Context, params MarkReadParams) error
	MarkAllRead(ctx context.Context, params MarkAllReadParams) error
	CreateNotification(ctx context.Context, params CreateNotificationParams) (accountdb.AccountNotification, error)

	// Payment Method
	CreatePaymentMethod(ctx context.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error)
	ListPaymentMethod(
		ctx context.Context,
		params ListPaymentMethodParams,
	) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error)
	UpdatePaymentMethod(ctx context.Context, params UpdatePaymentMethodParams) (accountdb.AccountPaymentMethod, error)
	DeletePaymentMethod(ctx context.Context, params DeletePaymentMethodParams) error
	SetDefaultPaymentMethod(
		ctx context.Context,
		params SetDefaultPaymentMethodParams,
	) (accountdb.AccountPaymentMethod, error)
	TokenizeCard(ctx context.Context, params TokenizeCardParams) (payment.TokenizeResult, error)

	// Wallet
	GetWalletBalance(ctx context.Context, accountID uuid.UUID) (int64, error)
	WalletDebit(ctx context.Context, params WalletDebitParams) (WalletDebitResult, error)
	WalletCredit(ctx context.Context, params WalletCreditParams) error
	ListWalletTransactions(ctx context.Context, params ListWalletTransactionsParams) ([]WalletTransactionResult, error)
}

type AccountStorage = pgsqlc.Storage[*accountdb.Queries]

// AccountHandler implements the core business logic for the account module.
type AccountHandler struct {
	tokenDuration        time.Duration
	jwtSecret            []byte
	refreshTokenDuration time.Duration
	refreshSecret        []byte

	config  *config.Config
	storage AccountStorage
	common  commonbiz.CommonBiz
}

func (b *AccountHandler) ServiceName() string {
	return "Account"
}

// NewAccountHandler creates a new AccountHandler with the given dependencies.
func NewAccountHandler(
	config *config.Config,
	storage AccountStorage,
	common commonbiz.CommonBiz,
) *AccountHandler {
	return &AccountHandler{
		tokenDuration:        time.Duration(config.App.JWT.AccessTokenDuration * int64(time.Second)),
		jwtSecret:            []byte(config.App.JWT.Secret),
		refreshTokenDuration: time.Duration(config.App.JWT.RefreshTokenDuration * int64(time.Second)),
		refreshSecret:        []byte(config.App.JWT.RefreshSecret),

		config:  config,
		storage: storage,
		common:  common,
	}
}

type ListWalletTransactionsParams struct {
	AccountID uuid.UUID `validate:"required"`
	Limit     int64     `validate:"min=1,max=100"`
	Offset    int64     `validate:"min=0"`
}

type WalletTransactionResult struct {
	ID          int64   `json:"id"`
	AccountID   string  `json:"account_id"`
	Type        string  `json:"type"`
	Amount      int64   `json:"amount"`
	ReferenceID *string `json:"reference_id,omitempty"`
	Note        *string `json:"note,omitempty"`
	DateCreated string  `json:"date_created"`
}
