package accountmodel

import (
	accountdb "shopnexus-remastered/internal/module/account/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	Account AuthenticatedAccount
}

type AuthenticatedAccount struct {
	Type accountdb.AccountType `validate:"required,validateFn=Valid"`
	ID   uuid.UUID             `validate:"required"`
}
