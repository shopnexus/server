package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// AddIdentityRun is a proven address, or a guess at an add_identity code,
// for the account AccountID. Key is the client's Idempotency-Key.
type AddIdentityRun struct {
	AccountID uuid.UUID        `json:"account_id"`
	Address   *contact.Address `json:"address,omitempty"`
	Guess     *CodeGuess       `json:"guess,omitempty"`
	Key       uuid.UUID        `json:"key,omitzero"`
}

// AddIdentityResult lists the account's identities after the add. A refusal
// travels as data.
type AddIdentityResult struct {
	Identities []Identity    `json:"identities"`
	Refused    *apperr.Error `json:"refused,omitempty"`
}

func (r AddIdentityResult) Refusal() *apperr.Error { return r.Refused }

// AddIdentity gives an account the identity kind it lacks. It never merges
// accounts: a value another account holds is identity_taken.
type AddIdentity struct {
	Prefix      string
	Challenges  repository.Challenges
	Credentials repository.Credentials
	Now         func() time.Time
}

// addIdentityService is the AddIdentity service's name without the prefix.
const addIdentityService = "AddIdentity"

func (s AddIdentity) ServiceName() string { return s.Prefix + addIdentityService }

// loadedCredentials is what the read step found.
type loadedCredentials struct {
	Credentials domain.Credentials `json:"credentials"`
	Found       bool               `json:"found"`
}

// saveOutcome is how the save step ended, for the flow to decide on.
type saveOutcome string

const (
	saved      saveOutcome = "saved"
	lostRace   saveOutcome = "lost_race"
	valueTaken saveOutcome = "taken"
)

// Run is UC-ACC-08, steps 2 and 3.
func (s AddIdentity) Run(ctx restate.Context, in AddIdentityRun) (AddIdentityResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return AddIdentityResult{}, err
	}
	var addr contact.Address
	// A1. Judge the code; E5 one that expired or is another account's.
	if in.Guess != nil {
		proved, refused, judgeErr := judgeGuess(ctx, s.Challenges, *in.Guess, domain.PurposeAddIdentity, &in.AccountID, in.Key, at)
		if judgeErr != nil || refused != nil {
			return AddIdentityResult{Refused: refused}, judgeErr
		}
		addr = proved.Address
	} else {
		addr = *in.Address
	}
	l, err := restate.Run(ctx, func(rc restate.RunContext) (loadedCredentials, error) {
		c, stepErr := s.Credentials.Get(rc, in.AccountID)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedCredentials{}, nil
		}
		if stepErr != nil {
			return loadedCredentials{}, fmt.Errorf("get credentials: %w", stepErr)
		}
		return loadedCredentials{Credentials: c, Found: true}, nil
	})
	if err != nil {
		return AddIdentityResult{}, fmt.Errorf("load credentials: %w", err)
	}
	if !l.Found {
		// Every account that signed in has credentials until Erase deletes
		// them: the account was deleted since the caller was checked.
		return AddIdentityResult{Refused: domain.ErrSessionRevoked}, nil
	}
	c := l.Credentials
	// The save's write ID derives from the request key, so a retry of the
	// request finds its identity added and answers as the first did.
	w := restatex.NewID(ctx, at)
	if in.Key != uuid.Nil() {
		w = restatex.DerivedID("identity-add", in.AccountID.String(), in.Key.String())
	}
	// A2 · NFR-IDEM-01
	if c.WriteID() == w {
		return AddIdentityResult{Identities: identitiesOf(c)}, nil
	}
	// 3. Add the identity; E3 a kind the account has · BR-ACC-05.
	if added := c.Add(domain.NewIdentity(restatex.NewID(ctx, at), addr, at)); added != nil {
		return AddIdentityResult{Refused: apperr.From(added)}, nil
	}
	outcome, err := restate.Run(ctx, func(rc restate.RunContext) (saveOutcome, error) {
		c := c // Save sets the write ID on its copy; c outside stays as journaled
		stepErr := s.Credentials.Save(rc, &c, w)
		switch {
		case errors.Is(stepErr, repository.ErrConflict):
			return lostRace, nil
		case errors.Is(stepErr, domain.ErrIdentityTaken):
			return valueTaken, nil
		case stepErr != nil:
			return "", fmt.Errorf("save credentials: %w", stepErr)
		default:
			return saved, nil
		}
	})
	if err != nil {
		return AddIdentityResult{}, fmt.Errorf("save the identity: %w", err)
	}
	switch outcome {
	case lostRace:
		// E6
		return AddIdentityResult{Refused: apperr.ErrTryAgain}, nil
	case valueTaken:
		// E4 · BR-ACC-05
		return AddIdentityResult{Refused: domain.ErrIdentityTaken}, nil
	case saved:
	default:
	}
	return AddIdentityResult{Identities: identitiesOf(c)}, nil
}
