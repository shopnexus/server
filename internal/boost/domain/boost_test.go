package domain_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/boost/domain"
)

func TestBoostMoves(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	day := domain.Every
	fresh := func() domain.Boost {
		return domain.NewBoost(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), domain.PackageOf(2, 15_000), uuid.NewV7(), at)
	}
	if p := domain.PackageOf(7, 29_000); p.ID != "7d" {
		t.Fatalf("package %+v", p)
	}

	b := fresh()
	if !b.Start(at) || b.Start(at) {
		t.Fatal("started twice")
	}
	st := b.State()
	if st.Bumps != 1 || !st.EndsAt.Equal(at.Add(2*day)) || !st.NextTickAt.Equal(at.Add(day)) {
		t.Fatalf("started %+v", st)
	}
	if b.Due(at) || !b.Due(at.Add(day)) || b.Last(at.Add(day)) {
		t.Fatal("first tick")
	}
	// A late tick keeps the day's rhythm and stops at the end.
	if next := b.Ticked(at.Add(day), at.Add(day+time.Hour), false); !next.Equal(at.Add(2*day)) || b.State().Bumps != 1 {
		t.Fatalf("a day not on sale: %v %+v", next, b.State())
	}
	if !b.Last(at.Add(2 * day)) {
		t.Fatal("the end tick")
	}
	b.End(at.Add(2 * day))
	if b.Status() != domain.StatusEnded || b.NextTick() != nil || b.Refuse(at) {
		t.Fatalf("ended %+v", b.State())
	}

	c := fresh()
	if !c.Cancel(at) || c.Close(domain.StatusFailed, at) || !c.Refuse(at) || !c.Refunded(at) || c.Status() != domain.StatusRefunded {
		t.Fatalf("cancelled then paid %+v", c.State())
	}
	e := fresh()
	if e.Close(domain.StatusActive, at) || !e.Close(domain.StatusExpired, at) || e.Status() != domain.StatusExpired {
		t.Fatalf("expired %+v", e.State())
	}
}
