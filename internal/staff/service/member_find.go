package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

// FindMember reads the member with email, in any case and spacing; an
// unknown one is domain.ErrMemberNotFound.
func (s *Staff) FindMember(ctx context.Context, email string) (MemberView, error) {
	m, err := s.members.FindByEmail(ctx, contact.NormalizeEmail(email))
	if errors.Is(err, repository.ErrNotFound) {
		return MemberView{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return MemberView{}, fmt.Errorf("find staff member: %w", err)
	}
	return viewOf(m, nil), nil
}
