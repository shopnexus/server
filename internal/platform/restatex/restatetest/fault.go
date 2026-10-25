package restatetest

import (
	"errors"
	"sync/atomic"
)

// CommitThenFail turns the first successful store call into a retryable
// error, as when the process dies after the write committed and before
// Restate journaled the step. A test embeds it in a store wrapper and
// returns After(err) from the method under test; the retried step must then
// land the same row.
type CommitThenFail struct{ failed atomic.Bool }

func (c *CommitThenFail) After(err error) error {
	if err == nil && !c.failed.Swap(true) {
		return errors.New("connection reset after commit")
	}
	return err
}
