package carrier_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/provider/carrier"
)

func TestNoneMakesAndReadsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := carrier.None{}
	if _, err := n.Create(ctx, carrier.CreateInput{OrderID: uuid.NewV7()}); !errors.Is(err, carrier.ErrUnavailable) {
		t.Errorf("create: %v", err)
	}
	if _, err := n.Lookup(ctx, "spx", "SPX1"); !errors.Is(err, carrier.ErrUnavailable) {
		t.Errorf("lookup: %v", err)
	}
	if err := n.Cancel(ctx, "ghtk", "GHTK-1"); err != nil {
		t.Errorf("cancel: %v", err)
	}
}
