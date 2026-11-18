package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/contact"
)

// FindMember reads the member with email, in any case and spacing; an
// unknown one is domain.ErrMemberNotFound.
func (s *Console) FindMember(ctx context.Context, email string) (MemberView, error) {
	m, err := s.members.FindByEmail(ctx, contact.NormalizeEmail(email))
	if errors.Is(err, repository.ErrNotFound) {
		return MemberView{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return MemberView{}, fmt.Errorf("find console member: %w", err)
	}
	return viewOf(m, nil), nil
}
