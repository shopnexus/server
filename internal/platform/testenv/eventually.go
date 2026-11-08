package testenv

import (
	"testing"
	"time"
)

// Eventually calls check every 50 ms until it returns nil and fails tb
// with the last error when within passes first. It is for effects that
// land asynchronously, such as a Restate invocation or an event the bus
// delivers.
func Eventually(tb testing.TB, within time.Duration, check func() error) {
	tb.Helper()
	var err error
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err = check(); err == nil {
			return
		}
	}
	tb.Fatalf("not within %s: %v", within, err)
}
