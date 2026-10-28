package service

import (
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
)

// SignInInput is a phone token from Firebase. A repeated IdempotencyKey with
// a token for the same number gets the first request's account.
type SignInInput struct {
	IdempotencyKey string
	PhoneToken     string
	DeviceName     string
}

// SignedIn is the SignIn response in api/schemas/auth.yaml.
type SignedIn struct {
	domain.Tokens

	Account SignedInAccount `json:"account"`
}

type SignedInAccount struct {
	ID    uuid.UUID `json:"id"`
	IsNew bool      `json:"is_new"`
}

// SessionItem is the Session schema in api/schemas/auth.yaml: one device in
// the caller's list.
type SessionItem struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// Identity is a sign-in method as staff see it: the Identity schema in
// api/schemas/staff.yaml.
type Identity struct {
	Kind  domain.Kind `json:"kind"`
	Value string      `json:"value"`
}
