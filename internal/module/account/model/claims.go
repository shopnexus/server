package accountmodel

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	Account AuthenticatedAccount
}

type AuthenticatedAccount struct {
	ID     uuid.UUID `validate:"required"`
	Number int64     `validate:"required"`
}
