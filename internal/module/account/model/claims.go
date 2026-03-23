package accountmodel

import (
	accountdb "shopnexus-server/internal/module/account/db/sqlc"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	Account AuthenticatedAccount
}

type AuthenticatedAccount struct {
	Type   accountdb.AccountType `validate:"required,validateFn=Valid"`
	ID     uuid.UUID             `validate:"required"`
	Number int64                 `validate:"required"`
}
