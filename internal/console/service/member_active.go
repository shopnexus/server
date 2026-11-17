package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/console/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
)

// ActiveMember reads the member on every call, as Authenticate reads the
// session, so a disable stops the agent's next read for that member.
func (s *Console) ActiveMember(ctx context.Context, id uuid.UUID) (Me, error) {
	m, err := s.members.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return Me{}, authn.ErrUnauthenticated
	}
	if err != nil {
		return Me{}, fmt.Errorf("get console member: %w", err)
	}
	if m.Status() != domain.StatusActive {
		return Me{}, authn.ErrUnauthenticated
	}
	return meOf(m), nil
}
