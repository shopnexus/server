package otp_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/otp/otptest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestPGChallenges(t *testing.T) {
	otptest.RunChallengeContract(t, func(t *testing.T) otp.ChallengeStore { return otp.NewChallengePGStore(pgtest.New(t)) })
}

func TestPGQuotas(t *testing.T) {
	otptest.RunQuotaContract(t, func(t *testing.T) otp.QuotaStore { return otp.NewQuotaPGStore(pgtest.New(t)) })
}
