package handler

import (
	"uuid"

	"github.com/shopnexus/server/internal/account/service"
)

// proofBody is the proof fields of a sign-in, add-identity or
// reauthentication body: SignInRequest or ProofRequest in the spec. The
// spec validator has checked that exactly one is set.
type proofBody struct {
	PhoneToken  string         `json:"phone_token"`
	GoogleToken string         `json:"google_token"`
	EmailCode   *emailCodeBody `json:"email_code"`
}

type emailCodeBody struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
}

func (b proofBody) proof() service.Proof {
	p := service.Proof{PhoneToken: b.PhoneToken, GoogleToken: b.GoogleToken}
	if b.EmailCode != nil {
		p.EmailCode = &service.EmailCode{ChallengeID: b.EmailCode.ChallengeID, Code: b.EmailCode.Code}
	}
	return p
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
