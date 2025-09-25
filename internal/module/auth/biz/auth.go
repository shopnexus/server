package authbiz

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/cachestruct"
	"shopnexus-remastered/internal/db"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"

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

	accountBiz *accountbiz.AccountBiz
	cache      cachestruct.Client
}

func NewAuthBiz(accountBiz *accountbiz.AccountBiz, cache cachestruct.Client) *AuthBiz {
	return &AuthBiz{
		tokenDuration:        time.Duration(config.GetConfig().App.JWT.AccessTokenDuration * int64(time.Second)),
		jwtSecret:            []byte(config.GetConfig().App.JWT.Secret),
		refreshTokenDuration: time.Duration(config.GetConfig().App.JWT.RefreshTokenDuration * int64(time.Second)),
		refreshSecret:        []byte(config.GetConfig().App.JWT.RefreshSecret),
		accountBiz:           accountBiz,
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
	Password null.String `validate:"required,min=8,max=72"`
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

	account, err := a.accountBiz.FindAccount(ctx, accountbiz.FindAccountParams{
		Username: params.Username,
		Email:    params.Email,
		Phone:    params.Phone,
	})
	if err != nil {
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

	// Hash the password if provided
	var hashedPassword null.String
	if params.Password.Valid {
		hashed, err := a.CreateHash(params.Password.String)
		if err != nil {
			return zero, err
		}
		hashedPassword.SetValid(hashed)
	}

	account, err := a.accountBiz.CreateAccount(ctx, accountbiz.CreateAccountParams{
		Type:     params.Type,
		Username: params.Username,
		Email:    params.Email,
		Phone:    params.Phone,
		Password: hashedPassword,
	})
	if err != nil {
		return zero, err
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
	claims, err := ValidateAccessToken(config.GetConfig().App.JWT.RefreshSecret, refreshToken)
	if err != nil {
		return zero, err
	}

	account, err := a.accountBiz.FindAccount(ctx, accountbiz.FindAccountParams{
		ID: null.NewInt(claims.Account.ID, true),
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
