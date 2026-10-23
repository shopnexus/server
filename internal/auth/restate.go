package auth

import (
	"github.com/restatedev/sdk-go/ingress"
)

// Restate is how handlers reach auth's Restate services.
type Restate struct {
	Client *ingress.Client
	Prefix string
}

func (r Restate) OTP() ingress.Requester[OTPSendInput, OTPSendResult] {
	return ingress.Service[OTPSendInput, OTPSendResult](r.Client, r.Prefix+otpService, "Send")
}

func (r Restate) SignIn() ingress.Requester[SignInInput, SignInResult] {
	return ingress.Service[SignInInput, SignInResult](r.Client, r.Prefix+signInService, "Run")
}
