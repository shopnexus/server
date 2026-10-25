package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

// Authenticate returns the live session whose cookie holds tok. It reads the
// session and the member on every call, so a disable, a reset or a sign-out
// stops the next request. A session that does not live is
// authn.ErrUnauthenticated.
func (s *Staff) Authenticate(ctx context.Context, tok string) (domain.Grant, error) {
	g, err := s.sessions.Live(ctx, token.Hash(tok), s.now())
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Grant{}, authn.ErrUnauthenticated
	}
	if err != nil {
		return domain.Grant{}, fmt.Errorf("read staff session: %w", err)
	}
	return g, nil
}
