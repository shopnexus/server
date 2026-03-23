package accountbiz

import (
	"context"
	"errors"
	"fmt"
	"shopnexus-server/config"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountmodel "shopnexus-server/internal/module/account/model"
	authclaims "shopnexus-server/internal/shared/claims"
	"shopnexus-server/internal/shared/validator"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// CreateClaims generates JWT claims for the given account.
func (a *AccountBiz) CreateClaims(account accountdb.AccountAccount) accountmodel.Claims {
	return accountmodel.Claims{
		Account: accountmodel.AuthenticatedAccount{
			Type:   account.Type,
			ID:     account.ID,
			Number: account.Number,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    "shopnexus",
			Subject:   account.ID.String(),
			Audience:  []string{"shopnexus"},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.tokenDuration)),
		},
	}
}

// GenerateAccessToken creates a JWT access token for the given account.
func (a *AccountBiz) GenerateAccessToken(account accountdb.AccountAccount) (string, error) {
	claims := a.CreateClaims(account)
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)

	signedToken, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

// CreateRefreshClaims generates JWT claims for refresh token
func (a *AccountBiz) CreateRefreshClaims(account accountdb.AccountAccount) accountmodel.Claims {
	return accountmodel.Claims{
		Account: accountmodel.AuthenticatedAccount{
			Type:   account.Type,
			ID:     account.ID,
			Number: account.Number,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   account.ID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.refreshTokenDuration)),
		},
	}
}

// GenerateRefreshToken creates a JWT refresh token
func (a *AccountBiz) GenerateRefreshToken(account accountdb.AccountAccount) (string, error) {
	claims := a.CreateRefreshClaims(account)
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signedToken, err := token.SignedString(a.refreshSecret)
	if err != nil {
		return "", err
	}
	return signedToken, nil
}

// ComparePassword checks if the provided password matches the hashed password.
func (a *AccountBiz) ComparePassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// CreateHash generates a hashed password (currently using bcrypt).
func (a *AccountBiz) CreateHash(password string) (string, error) {
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
	Account      accountdb.AccountAccount
	AccessToken  string
	RefreshToken string
}

func (a *AccountBiz) Login(ctx context.Context, params LoginParams) (LoginResult, error) {
	var zero LoginResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if !params.Username.Valid && !params.Email.Valid && !params.Phone.Valid {
		return zero, fmt.Errorf("at least one of username, email, or phone must be provided")
	}

	account, err := a.storage.Querier().GetAccount(ctx, accountdb.GetAccountParams{
		Phone:    params.Phone,
		Email:    params.Email,
		Username: params.Username,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, accountmodel.ErrAccountNotFound
		}
		return zero, err
	}

	// If the account has a password, require it for login
	if account.Password.Valid {
		if !params.Password.Valid {
			return zero, accountmodel.ErrInvalidCredentials
		}
		if !a.ComparePassword(account.Password.String, params.Password.String) {
			return zero, accountmodel.ErrInvalidCredentials
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
	Type     accountdb.AccountType `validate:"required,validateFn=Valid"`
	Username null.String           `validate:"omitnil,min=1,max=255"`
	Email    null.String           `validate:"omitnil,email"`
	Phone    null.String           `validate:"omitnil,e164"`
	Password null.String           `validate:"required,min=8,max=72"`
}

type RegisterResult struct {
	Account      accountdb.AccountAccount
	AccessToken  string
	RefreshToken string
}

func (a *AccountBiz) Register(ctx context.Context, params RegisterParams) (RegisterResult, error) {
	var zero RegisterResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if !params.Username.Valid && !params.Email.Valid && !params.Phone.Valid {
		return zero, accountmodel.ErrMissingIdentifier
	}

	// If register via Google OAuth, password can be nil => password is nil, email is required
	//! More oauth providers can be added in the future
	if !params.Password.Valid && !params.Email.Valid {
		return zero, fmt.Errorf("email is required when password is not provided")
	}

	// Hash the password if provided
	var hashedPassword null.String
	if params.Password.Valid {
		hashed, err := a.CreateHash(params.Password.String)
		if err != nil {
			return zero, err
		}
		hashedPassword.SetValid(hashed)
	}

	// Create account base
	account, err := a.storage.Querier().CreateDefaultAccount(ctx, accountdb.CreateDefaultAccountParams{
		Type:     params.Type,
		Phone:    params.Phone,
		Email:    params.Email,
		Username: params.Username,
		Password: hashedPassword,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to register account: %w", err)
	}

	// Create empty profile
	if _, err = a.storage.Querier().CreateDefaultProfile(ctx, accountdb.CreateDefaultProfileParams{
		ID: account.ID,
	}); err != nil {
		return zero, fmt.Errorf("failed to register account: %w", err)
	}

	// Create empty customer/vendor additional profile
	switch account.Type {
	case accountdb.AccountTypeCustomer:
		_, err = a.storage.Querier().CreateDefaultCustomer(ctx, account.ID)
	case accountdb.AccountTypeVendor:
		_, err = a.storage.Querier().CreateDefaultVendor(ctx, account.ID)
	default:
		return zero, fmt.Errorf("unsupported account type: %v", account.Type)
	}
	if err != nil {
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
func (a *AccountBiz) Refresh(ctx context.Context, refreshToken string) (RefreshResult, error) {
	var zero RefreshResult
	claims, err := authclaims.ValidateAccessToken(config.GetConfig().App.JWT.RefreshSecret, refreshToken)
	if err != nil {
		return zero, err
	}

	account, err := a.storage.Querier().GetAccount(ctx, accountdb.GetAccountParams{
		ID: uuid.NullUUID{UUID: claims.Account.ID, Valid: true},
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
