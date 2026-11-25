package service

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/mailview"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/email"
)

// EmailCodeInput is an address to mail a code to. AccountID names the
// account an add_identity code is for and is nil for sign_in. Key is the
// client's Idempotency-Key.
type EmailCodeInput struct {
	Email     contact.Address `json:"email"`
	Purpose   domain.Purpose  `json:"purpose"`
	AccountID *uuid.UUID      `json:"account_id,omitempty"`
	IP        netip.Addr      `json:"ip"`
	// Locale is the language of the mail; a journal entry from before reads
	// as Vietnamese.
	Locale locale.Tag `json:"locale,omitempty"`
	Key    uuid.UUID  `json:"key,omitzero"`
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

//go:embed mail/*.json
var mailFiles embed.FS

// mailWords is the code mail's text in every language, mail/<tag>.json.
var mailWords = locale.MustLoad(mailFiles, "mail")

// EmailCodeSend mails sign-in and add-identity codes. It knows nothing of
// accounts, so its answer is the same whether or not one holds the address.
type EmailCodeSend struct {
	Prefix     string
	Quotas     repository.Quotas
	Challenges repository.Challenges
	Codes      domain.Codes
	Mail       mailview.Mailer
	Now        func() time.Time
	Log        *slog.Logger
}

// mailOutcome is how the mail step ended.
type mailOutcome string

const (
	mailSent    mailOutcome = "sent"
	mailExpired mailOutcome = "expired"
	mailRefused mailOutcome = "refused"
)

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
	// send stays spent even when a later step fails, and a retry of the key
	// that finds its own save spends nothing (K2).
	if refused, admitErr := admit(ctx, s.Quotas, addressKey(in.IP), in.Key, at, addressRules); admitErr != nil || refused != nil {
		return EmailCodeResult{Refused: refused}, admitErr
	}
	if refused, admitErr := admit(ctx, s.Quotas, "dest:"+in.Email.Value(), in.Key, at, destinationRules); admitErr != nil || refused != nil {
		return EmailCodeResult{Refused: refused}, admitErr
	}
	// The code is derived from the challenge ID, so a retried send step
	// mails the same code, and the code is never stored or journaled.
	id := restatex.NewID(ctx, at)
	c := domain.NewChallenge(id, in.Purpose, in.Email, in.AccountID, in.IP, s.Codes.Hash(in.Email, s.Codes.For(id)), at)
	stored, err := restate.Run(ctx, func(rc restate.RunContext) (uuid.UUID, error) {
		return s.Challenges.Create(rc, c, in.Key)
	})
	if err != nil {
		return EmailCodeResult{}, fmt.Errorf("create challenge: %w", err)
	}
	if stored != id {
		// A retry of the key raced the first request, which mails its code.
		first, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Challenge, error) {
			return s.Challenges.Get(rc, stored)
		})
		if err != nil {
			return EmailCodeResult{}, fmt.Errorf("get the key's challenge: %w", err)
		}
		return EmailCodeResult{ChallengeID: stored, ExpiresAt: first.ExpiresAt(), ResendAt: first.CreatedAt().Add(resend.Window)}, nil
	}
	// A retry after the code expired mails nothing, and a provider that
	// refuses the message for good ends the send: retrying would never stop.
	mailed, err := restate.Run(ctx, func(rc restate.RunContext) (mailOutcome, error) {
		if !s.Now().Before(c.ExpiresAt()) {
			return mailExpired, nil
		}
		code, tag := s.Codes.For(id), locale.Of(string(in.Locale))
		say := func(key string, args ...any) string { return mailWords.Say(tag, key, args...) }
		l := mailview.Letter{
			Locale: tag, Group: say("group"), Art: mailview.ArtVerified, Code: code, CodeNote: say("expires"),
			Preheader: say("preheader", code),
			Subject:   say("sign_in.subject", code),
			Title:     say("sign_in.title"),
			Lead:      say("sign_in.lead"),
			Note:      say("sign_in.note"),
			Why:       say("sign_in.why"),
		}
		if in.Purpose == domain.PurposeAddIdentity {
			l.Subject = say("add.subject", code)
			l.Title = say("add.title")
			l.Lead = say("add.lead", in.Email.Value())
			l.Note = say("add.note")
			l.Why = say("add.why")
		}
		stepErr := s.Mail.Send(rc, in.Email.Value(), id.String(), l)
		if errors.Is(stepErr, email.ErrRefused) {
			s.Log.WarnContext(rc, "mail refused", "to", in.Email, "err", stepErr)
			return mailRefused, nil
		}
		if stepErr != nil {
			return "", fmt.Errorf("mail the code: %w", stepErr)
		}
		codesSent.Add(rc, 1)
		return mailSent, nil
	})
	if err != nil {
		return EmailCodeResult{}, fmt.Errorf("send code: %w", err)
	}
	if mailed == mailRefused {
		return EmailCodeResult{Refused: contact.ErrInvalidEmail}, nil
	}
	return EmailCodeResult{ChallengeID: id, ExpiresAt: c.ExpiresAt(), ResendAt: at.Add(resend.Window)}, nil
}

// admit spends one send from the quota at key. It reads the quota in one
// step, decides between steps and saves in another, so a retried save does
// not count the send twice. With a request key the save's write ID derives
// from it, so a racing retry of that key finds its own save and is admitted
// again without spending. It retries a lost race three times, so parallel
// sends behind one NAT do not get a spurious 429.
func admit(ctx restate.Context, quotas repository.Quotas, key string, request uuid.UUID, at time.Time, rules []domain.Rule) (*apperr.Error, error) {
	own := uuid.Nil()
	if request != uuid.Nil() {
		own = restatex.DerivedID("otp-quota", key, request.String())
	}
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
		if own != uuid.Nil() && q.WriteID() == own {
			return nil, nil //nolint:nilnil // This request's send is counted already.
		}
		rule, wait, ok := q.Admit(at, rules...)
		if !ok {
			refused := domain.ErrOTPLimit
			if rule == resend {
				refused = domain.ErrOTPTooSoon
			}
			return refused.WithRetryAfter(wait), nil
		}
		// NewID runs either way, so a replay draws the same IDs after it.
		w := restatex.NewID(ctx, at)
		if own != uuid.Nil() {
			w = own
		}
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
