package authbiz

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/pubsub"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/golang-jwt/jwt/v5"
	"github.com/guregu/null/v6"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/context"
)

type AuthBiz struct {
	tokenDuration        time.Duration
	jwtSecret            []byte
	refreshTokenDuration time.Duration
	refreshSecret        []byte

	storage pgsqlc.Storage
	pubsub  pubsub.Client
	cache   cachestruct.Client
}

func NewAuthBiz(
	storage pgsqlc.Storage,
	pubsub pubsub.Client,
	cache cachestruct.Client,
) *AuthBiz {
	return &AuthBiz{
		tokenDuration:        time.Duration(config.GetConfig().App.JWT.AccessTokenDuration * int64(time.Second)),
		jwtSecret:            []byte(config.GetConfig().App.JWT.Secret),
		refreshTokenDuration: time.Duration(config.GetConfig().App.JWT.RefreshTokenDuration * int64(time.Second)),
		refreshSecret:        []byte(config.GetConfig().App.JWT.RefreshSecret),
		storage:              storage,
		pubsub:               pubsub,
		cache:                cache,
	}
}

// CreateClaims generates JWT claims for the given account.
func (a *AuthBiz) CreateClaims(account db.AccountBase) authmodel.Claims {
	return authmodel.Claims{
		Account: authmodel.AuthenticatedAccount{
			Type: account.Type,
			ID:   account.ID,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    "shopnexus",
			Subject:   strconv.Itoa(int(account.ID)),
			Audience:  []string{"shopnexus"},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.tokenDuration)),
		},
	}
}

// GenerateAccessToken creates a JWT access token for the given account.
func (a *AuthBiz) GenerateAccessToken(account db.AccountBase) (string, error) {
	claims := a.CreateClaims(account)
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)

	signedToken, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

// CreateRefreshClaims generates JWT claims for refresh token
func (a *AuthBiz) CreateRefreshClaims(account db.AccountBase) authmodel.Claims {
	return authmodel.Claims{
		Account: authmodel.AuthenticatedAccount{
			Type: account.Type,
			ID:   account.ID,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "shopnexus",
			Subject:   strconv.Itoa(int(account.ID)),
			Audience:  []string{"shopnexus:refresh"},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.refreshTokenDuration)),
		},
	}
}

// GenerateRefreshToken creates a JWT refresh token
func (a *AuthBiz) GenerateRefreshToken(account db.AccountBase) (string, error) {
	claims := a.CreateRefreshClaims(account)
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signedToken, err := token.SignedString(a.refreshSecret)
	if err != nil {
		return "", err
	}
	return signedToken, nil
}

// ComparePassword checks if the provided password matches the hashed password.
func (a *AuthBiz) ComparePassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// CreateHash generates a hashed password (currently using bcrypt).
func (a *AuthBiz) CreateHash(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

type LoginParams struct {
	Username null.String `validate:"omitnil"`
	Email    null.String `validate:"omitnil"`
	Phone    null.String `validate:"omitnil"`
	Password null.String `validate:"omitnil,min=8,max=72"`
}

type LoginResult struct {
	Account      db.AccountBase
	AccessToken  string
	RefreshToken string
}

func (a *AuthBiz) Login(ctx context.Context, params LoginParams) (LoginResult, error) {
	var zero LoginResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if !params.Username.Valid && !params.Email.Valid && !params.Phone.Valid {
		return zero, fmt.Errorf("at least one of username, email, or phone must be provided")
	}

	account, err := a.storage.GetAccountBase(ctx, db.GetAccountBaseParams{
		Phone:    pgutil.NullStringToPgText(params.Phone),
		Email:    pgutil.NullStringToPgText(params.Email),
		Username: pgutil.NullStringToPgText(params.Username),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, authmodel.ErrAccountNotFound
		}
		return zero, err
	}

	if account.Password.Valid && params.Password.Valid {
		if !a.ComparePassword(account.Password.String, params.Password.String) {
			return zero, authmodel.ErrInvalidCredentials
		}
	}

	accessToken, err := a.GenerateAccessToken(account)
	if err != nil {
		return zero, err
	}

	refreshToken, err := a.GenerateRefreshToken(account)
	if err != nil {
		return zero, err
	}

	return LoginResult{
		Account:      account,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

type RegisterParams struct {
	Storage  pgsqlc.Storage
	Type     db.AccountType `validate:"required,validateFn=Valid"`
	Username null.String    `validate:"omitnil,min=1,max=255"`
	Email    null.String    `validate:"omitnil,email"`
	Phone    null.String    `validate:"omitnil,e164"`
	Password null.String    `validate:"required,min=8,max=72"`
}

type RegisterResult struct {
	Account      db.AccountBase
	AccessToken  string
	RefreshToken string
}

func (a *AuthBiz) Register(ctx context.Context, params RegisterParams) (RegisterResult, error) {
	var zero RegisterResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if !params.Username.Valid && !params.Email.Valid && !params.Phone.Valid {
		return zero, authmodel.ErrMissingIdentifier
	}

	// If register via Google OAuth, password can be nil => password is nil, email is required
	//! More oauth providers can be added in the future
	if !params.Password.Valid && !params.Email.Valid {
		return zero, fmt.Errorf("email is required when password is not provided")
	}

	var account db.AccountBase

	if err := a.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		// Hash the password if provided
		var hashedPassword null.String
		if params.Password.Valid {
			hashed, err := a.CreateHash(params.Password.String)
			if err != nil {
				return err
			}
			hashedPassword.SetValid(hashed)
		}

		var err error
		// Create account base
		account, err = txStorage.CreateDefaultAccountBase(ctx, db.CreateDefaultAccountBaseParams{
			Type:     params.Type,
			Phone:    pgutil.NullStringToPgText(params.Phone),
			Email:    pgutil.NullStringToPgText(params.Email),
			Username: pgutil.NullStringToPgText(params.Username),
			Password: pgutil.NullStringToPgText(hashedPassword),
		})
		if err != nil {
			return err
		}

		// Create empty profile
		if _, err = txStorage.CreateDefaultAccountProfile(ctx, db.CreateDefaultAccountProfileParams{
			ID: account.ID,
		}); err != nil {
			return err
		}

		// Create empty customer/vendor additional profile
		switch account.Type {
		case db.AccountTypeCustomer:
			_, err = txStorage.CreateDefaultAccountCustomer(ctx, account.ID)
		case db.AccountTypeVendor:
			_, err = txStorage.CreateDefaultAccountVendor(ctx, account.ID)
		default:
			return fmt.Errorf("unsupported account type: %v", account.Type)
		}
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to register account: %w", err)
	}

	accessToken, err := a.GenerateAccessToken(account)
	if err != nil {
		return zero, err
	}

	refreshToken, err := a.GenerateRefreshToken(account)
	if err != nil {
		return zero, err
	}

	return RegisterResult{
		Account:      account,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

type RefreshResult struct {
	AccessToken  string
	RefreshToken string
}

// Refresh validates the provided refresh token, loads the account, and issues new tokens
func (a *AuthBiz) Refresh(ctx context.Context, refreshToken string) (RefreshResult, error) {
	var zero RefreshResult
	claims, err := authclaims.ValidateAccessToken(config.GetConfig().App.JWT.RefreshSecret, refreshToken)
	if err != nil {
		return zero, err
	}

	account, err := a.storage.GetAccountBase(ctx, db.GetAccountBaseParams{
		ID: pgutil.Int64ToPgInt8(claims.Account.ID),
	})
	if err != nil {
		return zero, err
	}

	access, err := a.GenerateAccessToken(account)
	if err != nil {
		return zero, err
	}
	nextRefresh, err := a.GenerateRefreshToken(account)
	if err != nil {
		return zero, err
	}

	return RefreshResult{
		AccessToken:  access,
		RefreshToken: nextRefresh,
	}, nil
}
