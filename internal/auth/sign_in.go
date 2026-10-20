package auth

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// VerifyChallenge checks a code hash against a login challenge and saves the
// attempt before it answers, so a wrong code stays counted. A write that loses
// a race returns ErrTryAgain whether the code was right or wrong, and that
// attempt is not counted.
func VerifyChallenge(ctx context.Context, challenges otp.ChallengeStore, id uuid.UUID, codeHash []byte, now time.Time, writeID uuid.UUID) (contact.Address, error) {
	c, err := challenges.Get(ctx, id)
	if errors.Is(err, otp.ErrNotFound) {
		return contact.Address{}, otp.ErrCodeExpired
	}
	if err != nil {
		return contact.Address{}, err
	}
	if c.Purpose() != otp.PurposeLogin {
		return contact.Address{}, otp.ErrCodeExpired
	}
	if c.WriteID() == writeID {
		// A retried step whose save already committed: report what that run
		// decided instead of judging the code again.
		return c.Destination(), savedOutcome(c)
	}
	newest, err := challenges.IsNewest(ctx, c)
	if err != nil {
		return contact.Address{}, err
	}
	if !newest {
		return contact.Address{}, otp.ErrCodeExpired
	}
	verifyErr := c.Verify(codeHash, now)
	if err := challenges.Save(ctx, &c, writeID); err != nil {
		if errors.Is(err, otp.ErrConflict) {
			return contact.Address{}, ErrTryAgain
		}
		return contact.Address{}, err
	}
	return c.Destination(), verifyErr
}

// SignInExisting lets an existing account in: it refuses a suspended account
// and records the identity's use. Only a person who just proved they hold the
// identity gets here, so account_suspended is safe to reveal.
func SignInExisting(ctx context.Context, accounts AccountPort, creds credentials.Store, accountID, identityID uuid.UUID, now time.Time, writeID uuid.UUID) error {
	st, err := accounts.Status(ctx, accountID)
	if err != nil {
		return err
	}
	if st == account.StatusSuspended {
		return account.ErrSuspended
	}
	c, err := creds.Get(ctx, accountID)
	if err != nil {
		return err
	}
	if err := c.MarkUsed(identityID, now); err != nil {
		return err
	}
	// last_used_at is a hint for the dormant check; losing it to a concurrent
	// sign-in of the same account costs nothing.
	if err := creds.Save(ctx, &c, writeID); err != nil && !errors.Is(err, credentials.ErrConflict) {
		return err
	}
	return nil
}

// savedOutcome is the result a committed Verify left on c.
func savedOutcome(c otp.Challenge) error {
	switch {
	case c.ConsumedAt() != nil:
		return nil
	case c.Attempts() >= otp.MaxAttempts:
		return otp.ErrCodeLocked
	default:
		return otp.ErrInvalidCode.WithDetail("attempts_left", otp.MaxAttempts-c.Attempts())
	}
}
