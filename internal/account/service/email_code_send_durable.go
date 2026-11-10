package service

import (
	"fmt"
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
)

// EmailCodeInput is an address to mail a code to. AccountID names the
// account an add_identity code is for and is nil for sign_in.
type EmailCodeInput struct {
	Email     contact.Address `json:"email"`
	Purpose   domain.Purpose  `json:"purpose"`
	AccountID *uuid.UUID      `json:"account_id,omitempty"`
	IP        netip.Addr      `json:"ip"`
}

// EmailCodeResult names the challenge the code belongs to. A refusal
// travels as data.
type EmailCodeResult struct {
	ChallengeID uuid.UUID     `json:"challenge_id"`
	ExpiresAt   time.Time     `json:"expires_at"`
	ResendAt    time.Time     `json:"resend_at"`
	Refused     *apperr.Error `json:"refused,omitempty"`
}

func (r EmailCodeResult) Refusal() *apperr.Error { return r.Refused }

// EmailCodeSend mails sign-in and add-identity codes. It knows nothing of
// accounts, so its answer is the same whether or not one holds the address.
type EmailCodeSend struct {
	Prefix     string
	Quotas     repository.Quotas
	Challenges repository.Challenges
	Codes      domain.Codes
	Email      email.Sender
	Now        func() time.Time
}

// emailCodeSendService is the EmailCodeSend service's name without the prefix.
const emailCodeSendService = "EmailCodeSend"

func (s EmailCodeSend) ServiceName() string { return s.Prefix + emailCodeSendService }

func (s EmailCodeSend) Run(ctx restate.Context, in EmailCodeInput) (EmailCodeResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return EmailCodeResult{}, err
	}
	// The client address goes first, so an address over its limit cannot
	// spend the quota of the mailboxes it names. A quota that admitted the
	// send stays spent even when a later step fails.
	if refused, err := admit(ctx, s.Quotas, addressKey(in.IP), at, addressRules); err != nil || refused != nil {
		return EmailCodeResult{Refused: refused}, err
	}
	if refused, err := admit(ctx, s.Quotas, "dest:"+in.Email.Value(), at, destinationRules); err != nil || refused != nil {
		return EmailCodeResult{Refused: refused}, err
	}
	// The code is derived from the challenge ID, so a retried send step
	// mails the same code, and the code is never stored or journaled.
	id := restatex.NewID(ctx, at)
	c := domain.NewChallenge(id, in.Purpose, in.Email, in.AccountID, in.IP, s.Codes.Hash(in.Email, s.Codes.For(id)), at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Challenges.Create(rc, c) }); err != nil {
		return EmailCodeResult{}, fmt.Errorf("create challenge: %w", err)
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		code := s.Codes.For(id)
		body := "Mã xác minh ShopNexus của bạn là " + code + ". Mã hết hạn sau 5 phút. Không chia sẻ mã này với ai."
		if stepErr := s.Email.Send(rc, in.Email.Value(), "Mã xác minh ShopNexus: "+code, body); stepErr != nil {
			return fmt.Errorf("mail the code: %w", stepErr)
		}
		codesSent.Add(rc, 1)
		return nil
	}); err != nil {
		return EmailCodeResult{}, fmt.Errorf("send code: %w", err)
	}
	return EmailCodeResult{ChallengeID: id, ExpiresAt: c.ExpiresAt(), ResendAt: at.Add(resend.Window)}, nil
}

// admit spends one send from the quota at key. It reads the quota in one
// step, decides between steps and saves in another, so a retried save does
// not count the send twice. It retries a lost race three times, so parallel
// sends behind one NAT do not get a spurious 429.
func admit(ctx restate.Context, quotas repository.Quotas, key string, at time.Time, rules []domain.Rule) (*apperr.Error, error) {
	for range 3 {
		q, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Quota, error) {
			q, stepErr := quotas.Get(rc, key)
			if stepErr != nil {
				return domain.Quota{}, fmt.Errorf("get quota: %w", stepErr)
			}
			return q, nil
		})
		if err != nil {
			return nil, fmt.Errorf("load quota: %w", err)
		}
		rule, wait, ok := q.Admit(at, rules...)
		if !ok {
			refused := domain.ErrOTPLimit
			if rule == resend {
				refused = domain.ErrOTPTooSoon
			}
			return refused.WithRetryAfter(wait), nil
		}
		w := restatex.NewID(ctx, at)
		lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
			q := q // Save sets the write ID on its copy; q outside stays as journaled
			return restatex.Outcome(quotas.Save(rc, &q, w), repository.ErrConflict)
		})
		if err != nil {
			return nil, fmt.Errorf("save quota: %w", err)
		}
		if !lost {
			return nil, nil //nolint:nilnil // A nil refusal means the send was admitted.
		}
	}
	// The racing sends finish within a second.
	return domain.ErrOTPLimit.WithRetryAfter(time.Second), nil
}

var (
	// resend refuses with domain.ErrOTPTooSoon; every other rule with
	// domain.ErrOTPLimit.
	resend           = domain.Rule{Window: 45 * time.Second, Max: 1}
	destinationRules = []domain.Rule{resend, {Window: time.Hour, Max: 5}}
	addressRules     = []domain.Rule{{Window: time.Hour, Max: 20}}
)

// addressKey limits an IPv6 client by its /64, the block one host usually
// holds.
func addressKey(ip netip.Addr) string {
	if ip.Is4() {
		return "ip:" + ip.String()
	}
	p, _ := ip.Prefix(64)
	return "ip:" + p.String()
}
