package restatex

import (
	"errors"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// Refused is what a step decided: a business refusal, or none. A step
// returns it as data, since Restate retries any error a step returns. The
// JSON key is "refused", which journals written before this type still use.
type Refused struct {
	Refusal *apperr.Error `json:"refused,omitempty"`
}

// AsRefusal splits the error of a call to another module's service for a
// step to return: an *apperr.Error other than an internal one is a business
// refusal and comes back as data, and any other error is a fault for
// Restate to retry.
func AsRefusal(err error) (Refused, error) {
	if e, ok := refusal(err); ok {
		return Refused{Refusal: e}, nil
	}
	return Refused{}, err
}

// IsRefusal tells a business refusal, as AsRefusal takes one, from a
// fault that a step retries.
func IsRefusal(err error) bool {
	_, ok := refusal(err)
	return ok
}

func refusal(err error) (*apperr.Error, bool) {
	e, ok := errors.AsType[*apperr.Error](err)
	return e, ok && e.Kind != apperr.KindInternal
}
