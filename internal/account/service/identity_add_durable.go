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
// for the account AccountID.
type AddIdentityRun struct {
	AccountID uuid.UUID        `json:"account_id"`
	Address   *contact.Address `json:"address,omitempty"`
	Guess     *CodeGuess       `json:"guess,omitempty"`
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

func (s AddIdentity) Run(ctx restate.Context, in AddIdentityRun) (AddIdentityResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return AddIdentityResult{}, err
	}
	var addr contact.Address
	if in.Guess != nil {
		proved, refused, judgeErr := judgeGuess(ctx, s.Challenges, *in.Guess, domain.PurposeAddIdentity, &in.AccountID, at)
		if judgeErr != nil || refused != nil {
			return AddIdentityResult{Refused: refused}, judgeErr
		}
		addr = proved
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
	if added := c.Add(domain.NewIdentity(restatex.NewID(ctx, at), addr, at)); added != nil {
		return AddIdentityResult{Refused: apperr.From(added)}, nil
	}
	w := restatex.NewID(ctx, at)
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
		return AddIdentityResult{Refused: apperr.ErrTryAgain}, nil
	case valueTaken:
		return AddIdentityResult{Refused: domain.ErrIdentityTaken}, nil
	case saved:
	default:
	}
	ids := c.Identities()
	out := make([]Identity, 0, len(ids))
	for _, i := range ids {
		out = append(out, Identity{Kind: i.Kind(), Value: i.Value()})
	}
	return AddIdentityResult{Identities: out}, nil
}
