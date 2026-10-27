package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var now = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func TestNewAllowsEmptyName(t *testing.T) {
	t.Parallel()
	a, err := domain.New(uuid.NewV7(), "", now)
	if err != nil || a.DisplayName() != "" || a.Status(now) != domain.StatusActive {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestNewTrimsName(t *testing.T) {
	t.Parallel()
	a, err := domain.New(uuid.NewV7(), "  Linh  ", now)
	if err != nil || a.DisplayName() != "Linh" {
		t.Fatalf("%q %v", a.DisplayName(), err)
	}
}

func TestNameLengthCountsRunes(t *testing.T) {
	t.Parallel()
	if _, err := domain.New(uuid.NewV7(), strings.Repeat("ữ", 50), now); err != nil {
		t.Fatalf("50 Vietnamese characters rejected: %v", err)
	}
	_, err := domain.New(uuid.NewV7(), strings.Repeat("a", 51), now)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || e.Code != "invalid_account" || e.Fields["display_name"] == "" {
		t.Fatalf("51 characters: %v", err)
	}
}

func TestEditNameAndBio(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "", now)
	later := now.Add(time.Hour)
	name, bio := " Khánh Linh ", "  Đồ cũ còn tốt  "
	if err := a.Edit(&name, &bio, later); err != nil {
		t.Fatal(err)
	}
	if a.DisplayName() != "Khánh Linh" || a.Bio() != "Đồ cũ còn tốt" || !a.UpdatedAt().Equal(later) {
		t.Fatalf("%q %q %v", a.DisplayName(), a.Bio(), a.UpdatedAt())
	}
	empty := " "
	if err := a.Edit(nil, &empty, later); err != nil || a.Bio() != "" || a.DisplayName() != "Khánh Linh" {
		t.Fatalf("empty bio: %v %q %q", err, a.Bio(), a.DisplayName())
	}
}

// One invalid_account names every field that breaks a rule and changes
// nothing.
func TestEditRefusesEveryBrokenField(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	blank, long := "   ", strings.Repeat("ữ", 201)
	err := a.Edit(&blank, &long, now)
	e, ok := errors.AsType[*apperr.Error](err)
	if !ok || !errors.Is(err, domain.ErrInvalidAccount) || e.Fields["display_name"] == "" || e.Fields["bio"] == "" {
		t.Fatalf("%v", err)
	}
	if a.DisplayName() != "Linh" || a.Bio() != "" {
		t.Fatalf("changed: %q %q", a.DisplayName(), a.Bio())
	}
	fits := strings.Repeat("ữ", 200)
	if err = a.Edit(nil, &fits, now); err != nil {
		t.Fatalf("200 characters: %v", err)
	}
}

func TestSetAvatar(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	later := now.Add(time.Hour)
	a.SetAvatar("media/a.jpg", later)
	if a.AvatarKey() != "media/a.jpg" || !a.UpdatedAt().Equal(later) {
		t.Fatalf("%q %v", a.AvatarKey(), a.UpdatedAt())
	}
	a.SetAvatar("", later)
	if a.AvatarKey() != "" {
		t.Fatalf("not removed: %q", a.AvatarKey())
	}
}

func TestSuspendBlocksWrites(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	if err := a.Suspend("scam", nil, now); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(a.CanWrite(now), domain.ErrSuspended) || a.Status(now) != domain.StatusSuspended {
		t.Fatalf("status %s", a.Status(now))
	}
}

func TestSuspensionWithEndLapses(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	until := now.Add(time.Hour)
	a.Suspend("spam", &until, now)
	if a.CanWrite(now) == nil {
		t.Fatal("writes allowed during suspension")
	}
	if err := a.CanWrite(now.Add(2 * time.Hour)); err != nil || a.Status(now.Add(2*time.Hour)) != domain.StatusActive {
		t.Fatalf("after the end: %v %s", err, a.Status(now.Add(2*time.Hour)))
	}
}

func TestSuspendNeedsReason(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	if err := a.Suspend("  ", nil, now); !errors.Is(err, domain.ErrSuspendReason) {
		t.Fatalf("err %v", err)
	}
	if a.CanWrite(now) != nil {
		t.Fatal("a refused suspension still suspended")
	}
}

func TestSuspendEndInThePastRejected(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	past := now.Add(-time.Minute)
	if err := a.Suspend("spam", &past, now); !errors.Is(err, domain.ErrSuspendUntil) {
		t.Fatalf("err %v", err)
	}
}

func TestReinstate(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	a.Suspend("scam", nil, now)
	a.Reinstate(now)
	if a.CanWrite(now) != nil || a.SuspendReason() != "" || a.SuspendedUntil() != nil {
		t.Fatalf("%+v", a)
	}
}

func TestNewStartsWithItsIDAsWriteID(t *testing.T) {
	t.Parallel()
	id := uuid.NewV7()
	a, _ := domain.New(id, "Linh", now)
	if a.ID() != id || a.WriteID() != id {
		t.Fatalf("%+v", a)
	}
}
