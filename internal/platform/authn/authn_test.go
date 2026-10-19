package authn_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
)

func TestPrincipalRoundTrip(t *testing.T) {
	if _, ok := authn.From(context.Background()); ok {
		t.Fatal("bare context has a principal")
	}
	p := authn.Principal{AccountID: uuid.NewV7(), SessionID: uuid.NewV7(), AuthTime: time.Unix(100, 0)}
	got, ok := authn.From(authn.With(context.Background(), p))
	if !ok || got != p {
		t.Fatalf("got %+v %v", got, ok)
	}
}
