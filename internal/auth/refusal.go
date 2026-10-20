package auth

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// ErrTryAgain answers a request that collided with another write. It reads
// the same whether the collided OTP guess was right or wrong.
var ErrTryAgain = apperr.New(apperr.KindConflict, "try_again", "the request collided with another; try again")

// Refusal is a business error carried in a Restate result. The ingress turns
// a terminal error into a bare 500, so refusals travel as data.
type Refusal struct {
	Kind    apperr.Kind    `json:"kind"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// refuse splits err: a business error becomes a Refusal, anything else stays
// an error, which Restate retries.
func refuse(err error) (*Refusal, error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return &Refusal{Kind: ae.Kind, Code: ae.Code, Message: ae.Message, Details: ae.Details}, nil
	}
	return nil, err
}

// Err turns the refusal back into the error httpx writes; nil stays nil.
func (r *Refusal) Err() error {
	if r == nil {
		return nil
	}
	return &apperr.Error{Kind: r.Kind, Code: r.Code, Message: r.Message, Details: r.Details}
}
