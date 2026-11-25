package handler

import (
	"github.com/shopnexus/server/internal/account/service"
)

// proofBody is the proof fields of a sign-in, add-identity or
// reauthentication body: SignInRequest or ProofRequest in the spec. The
// spec validator has checked that exactly one is set.
type proofBody struct {
	service.Proof
}

// method names the proof for login_total.
func (b proofBody) method() string {
	switch {
	case b.EmailCode != nil:
		return "email"
	case b.GoogleToken != "":
		return "google"
	default:
		return "phone"
	}
}
