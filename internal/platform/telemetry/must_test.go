package telemetry_test

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server2/internal/platform/telemetry"
)

func TestMustReturnsTheInstrument(t *testing.T) {
	t.Parallel()
	c := telemetry.Must(otel.Meter("test").Int64Counter("must_test_total"))
	if c == nil {
		t.Fatal("got a nil counter")
	}
}

func TestMustPanicsOnAnError(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("Must did not panic")
		}
	}()
	telemetry.Must(0, errors.New("bad name"))
}
