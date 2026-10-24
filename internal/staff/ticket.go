package staff

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
)

// issueTicket signs a login ticket at the member's credentials version and
// names the member's next step.
func issueTicket(k Keys, m *member.Member, now time.Time) (LoginStep, error) {
	t, exp, err := k.Signer.Sign(token.Claims{
		Subject: m.ID(), SessionID: uuid.UUID{}, AuthTime: time.Time{}, Version: m.CredentialsVersion(),
		Audience: AudienceLogin, ExpiresAt: time.Time{},
	}, TicketTTL, now)
	if err != nil {
		return LoginStep{}, fmt.Errorf("sign login ticket: %w", err)
	}
	return LoginStep{Ticket: t, Next: m.Next(), TicketExpiresAt: exp}, nil
}

// readTicket returns the member a ticket names while the member is active
// and the ticket carries their current credentials version. A password
// change, a reset or a disable since the ticket was issued ends it.
func readTicket(ctx context.Context, members member.Store, k Keys, ticket string, now time.Time) (member.Member, error) {
	c, err := k.Verifier.Verify(ticket, AudienceLogin, now)
	if err != nil {
		return member.Member{}, ErrInvalidLoginTicket
	}
	m, err := members.Get(ctx, c.Subject)
	if errors.Is(err, member.ErrNotFound) {
		return member.Member{}, ErrInvalidLoginTicket
	}
	if err != nil {
		return member.Member{}, fmt.Errorf("get staff member: %w", err)
	}
	if m.Status() != member.StatusActive || m.CredentialsVersion() != c.Version {
		return member.Member{}, ErrInvalidLoginTicket
	}
	return m, nil
}
