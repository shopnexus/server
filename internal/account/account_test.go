package account_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

var now = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func TestNewAllowsEmptyName(t *testing.T) {
	t.Parallel()
	a, err := account.New(uuid.NewV7(), "", now)
	if err != nil || a.DisplayName() != "" || a.Status(now) != account.StatusActive {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestNewTrimsName(t *testing.T) {
	t.Parallel()
	a, err := account.New(uuid.NewV7(), "  Linh  ", now)
	if err != nil || a.DisplayName() != "Linh" {
		t.Fatalf("%q %v", a.DisplayName(), err)
	}
}

func TestNameLengthCountsRunes(t *testing.T) {
	t.Parallel()
	if _, err := account.New(uuid.NewV7(), strings.Repeat("ữ", 50), now); err != nil {
		t.Fatalf("50 Vietnamese characters rejected: %v", err)
	}
	if _, err := account.New(uuid.NewV7(), strings.Repeat("a", 51), now); !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("51 characters: %v", err)
	}
}

func TestSetDisplayName(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "", now)
	later := now.Add(time.Hour)
	if err := a.SetDisplayName("Khánh Linh", later); err != nil {
		t.Fatal(err)
	}
	if a.DisplayName() != "Khánh Linh" || !a.UpdatedAt().Equal(later) {
		t.Fatalf("%q %v", a.DisplayName(), a.UpdatedAt())
	}
	if err := a.SetDisplayName("   ", later); !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("blank name: %v", err)
	}
}

func TestSuspendBlocksWrites(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "Linh", now)
	if err := a.Suspend("scam", nil, now); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(a.CanWrite(now), account.ErrSuspended) || a.Status(now) != account.StatusSuspended {
		t.Fatalf("status %s", a.Status(now))
	}
}

func TestSuspensionWithEndLapses(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "Linh", now)
	until := now.Add(time.Hour)
	a.Suspend("spam", &until, now)
	if a.CanWrite(now) == nil {
		t.Fatal("writes allowed during suspension")
	}
	if err := a.CanWrite(now.Add(2 * time.Hour)); err != nil || a.Status(now.Add(2*time.Hour)) != account.StatusActive {
		t.Fatalf("after the end: %v %s", err, a.Status(now.Add(2*time.Hour)))
	}
}

func TestSuspendNeedsReason(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "Linh", now)
	if err := a.Suspend("  ", nil, now); !errors.Is(err, account.ErrSuspendReason) {
		t.Fatalf("err %v", err)
	}
	if a.CanWrite(now) != nil {
		t.Fatal("a refused suspension still suspended")
	}
}

func TestSuspendEndInThePastRejected(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "Linh", now)
	past := now.Add(-time.Minute)
	if err := a.Suspend("spam", &past, now); !errors.Is(err, account.ErrSuspendUntil) {
		t.Fatalf("err %v", err)
	}
}

func TestReinstate(t *testing.T) {
	t.Parallel()
	a, _ := account.New(uuid.NewV7(), "Linh", now)
	a.Suspend("scam", nil, now)
	a.Reinstate(now)
	if a.CanWrite(now) != nil || a.SuspendReason() != "" || a.SuspendedUntil() != nil {
		t.Fatalf("%+v", a)
	}
}

func TestNewStartsWithItsIDAsWriteID(t *testing.T) {
	t.Parallel()
	id := uuid.NewV7()
	a, _ := account.New(id, "Linh", now)
	if a.ID() != id || a.WriteID() != id {
		t.Fatalf("%+v", a)
	}
}
