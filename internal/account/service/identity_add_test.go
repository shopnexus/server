package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// Adding a sign-in method, and mailing the code that adds one, need a
// sign-in within 15 minutes and an account that may write. Both refuse
// before Restate is called, so these tests run without it.
func TestAddingAnIdentityNeedsARecentSignInAndAWritableAccount(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	svc := service.New(service.Deps{
		Accounts: accounts, Credentials: repository.NewCredentials(db), Now: func() time.Time { return now }, Log: discard,
	})
	ctx := context.Background()
	active, deleted := seed(t, accounts), seed(t, accounts)
	if _, err := db.Exec(ctx, `UPDATE accounts SET status = 'deleted', deleted_at = now(), display_name = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": deleted}); err != nil {
		t.Fatal(err)
	}
	caller := func(id uuid.UUID, signedIn time.Duration) authn.Principal {
		return authn.Principal{AccountID: id, SessionID: uuid.NewV7(), AuthTime: now.Add(-signedIn)}
	}
	for name, c := range map[string]struct {
		caller authn.Principal
		want   error
	}{
		"a sign-in 16 minutes ago": {caller(active, 16*time.Minute), domain.ErrReauthRequired},
		"a deleted account":        {caller(deleted, time.Minute), domain.ErrSessionRevoked},
	} {
		if _, err := svc.AddIdentity(ctx, service.AddIdentityInput{Key: uuid.NewV7(), Caller: c.caller, Proof: service.Proof{PhoneToken: "t"}}); !errors.Is(err, c.want) {
			t.Errorf("add identity, %s: %v", name, err)
		}
		p := c.caller
		if _, err := svc.SendEmailCode(ctx, service.SendEmailCodeInput{Key: uuid.NewV7(), Email: "linh@example.com", Caller: &p}); !errors.Is(err, c.want) {
			t.Errorf("send email code, %s: %v", name, err)
		}
	}
	if _, err := svc.Me(ctx, deleted); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Errorf("me, deleted: %v", err)
	}
}
