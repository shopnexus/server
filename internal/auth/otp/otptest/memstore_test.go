package otptest_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/otp/otptest"
)

func TestMemChallenges(t *testing.T) {
	otptest.RunChallengeContract(t, func(*testing.T) otp.ChallengeStore { return otptest.NewChallenges() })
}

func TestMemQuotas(t *testing.T) {
	otptest.RunQuotaContract(t, func(*testing.T) otp.QuotaStore { return otptest.NewQuotas() })
}
