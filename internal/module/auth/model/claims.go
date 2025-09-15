package authmodel

import (
	"shopnexus-remastered/internal/db"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	jwt.RegisteredClaims
	Account AuthenticatedAccount
}

type AuthenticatedAccount struct {
	Type db.AccountType `validate:"required,validateFn=Valid"`
	ID   int64          `validate:"required,gt=0"`
}
