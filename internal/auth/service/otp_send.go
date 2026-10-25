package service

import (
	"context"
)

// SendOTP sends a code. Restate runs it, so a crash between the quota and
// the message neither loses the code nor sends it twice.
func (a *Auth) SendOTP(ctx context.Context, in SendOTPInput) (OTPSent, error) {
	res, err := a.otp.Call(ctx, in.IdempotencyKey, OTPSendInput{
		Kind: in.Destination.Kind(), Destination: in.Destination.Value(), Purpose: in.Purpose, IP: in.IP,
	})
	if err != nil {
		return OTPSent{}, err
	}
	return OTPSent{ChallengeID: res.ChallengeID, ResendAfter: int(resend.Window.Seconds())}, nil
}
