// Package devauth signs accounts in on a dev stack without Firebase, for the
// dev tools devseed and loadsessions. Its phone verifier trusts the number a
// token spells, so no server ever uses it.
package devauth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/contact"
)

// Accounts is the account service with PhoneAsToken as its phone verifier.
// It refuses any stack but dev.
func Accounts(cfg config.Config, pool *pgxpool.Pool) (*service.Account, error) {
	if cfg.Env != config.EnvDev {
		return nil, fmt.Errorf("APP_ENV is %s; dev sign-in works on a dev stack only", cfg.Env)
	}
	return service.New(service.Deps{
		Accounts: repository.NewAccounts(pool), Credentials: repository.NewCredentials(pool), Sessions: repository.NewSessions(pool),
		Signer: domain.NewSigner(cfg.Auth), Phones: PhoneAsToken{}, Ingress: ingress.NewClient(cfg.Restate.IngressURL),
		Prefix: cfg.Restate.ServicePrefix, Now: time.Now, Log: slog.Default(),
	}), nil
}

// PhoneAsToken proves the phone number its token spells.
type PhoneAsToken struct{}

func (PhoneAsToken) Verify(_ context.Context, token string) (contact.Address, error) {
	addr, err := contact.ParsePhone(token)
	if err != nil {
		return contact.Address{}, fmt.Errorf("phone token %q: %w", token, err)
	}
	return addr, nil
}
