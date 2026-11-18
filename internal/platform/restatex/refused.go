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
	if e, ok := errors.AsType[*apperr.Error](err); ok && e.Kind != apperr.KindInternal {
		return Refused{Refusal: e}, nil
	}
	return Refused{}, err
}
