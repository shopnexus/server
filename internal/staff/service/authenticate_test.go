package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/staff/domain"
)

func TestAuthenticateFindsALiveSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	g, err := e.at(now).Authenticate(ctx, in.Token)
	if err != nil || g.StaffID != in.Me.ID || g.Role != domain.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := e.at(now).Authenticate(ctx, "ss_unknown"); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("an unknown token: %v", err)
	}
	if _, err := e.at(now.Add(domain.SessionTTL)).Authenticate(ctx, in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("after eight hours: %v", err)
	}
}

func TestAuthenticateEndsWithDisable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	if _, err := e.at(now).DisableMember(ctx, in.Me.ID, audit.ByCLI("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.at(now).Authenticate(ctx, in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("after the disable: %v", err)
	}
	if _, err := e.at(now).EnableMember(ctx, in.Me.ID, audit.ByCLI("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.at(now).Authenticate(ctx, in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("after enabling again: %v", err)
	}
}
