package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
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

func TestUnsuspend(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	a.Suspend("scam", nil, now)
	if changed, err := a.Unsuspend(now); !changed || err != nil {
		t.Fatalf("%v %v", changed, err)
	}
	if a.CanWrite(now) != nil || a.SuspendReason() != "" || a.SuspendedUntil() != nil {
		t.Fatalf("%+v", a)
	}
	if changed, err := a.Unsuspend(now); changed || err != nil {
		t.Fatalf("an active account: %v %v", changed, err)
	}
}

func TestUnsuspendRefusesADeletedAccount(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	a.Delete(now)
	if changed, err := a.Unsuspend(now); changed || !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("%v %v", changed, err)
	}
	if a.StoredStatus() != domain.StatusDeleted {
		t.Fatalf("status %s", a.StoredStatus())
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

func TestDeleteClearsTheProfile(t *testing.T) {
	t.Parallel()
	a, err := domain.New(uuid.NewV7(), "Linh", now)
	if err != nil {
		t.Fatal(err)
	}
	bio := "Bán đồ cũ"
	if err = a.Edit(nil, &bio, now); err != nil {
		t.Fatal(err)
	}
	a.SetAvatar("avatars/a.jpg", now)
	changed, err := a.Delete(now)
	if err != nil || !changed {
		t.Fatalf("delete: %v %v", changed, err)
	}
	if a.Status(now) != domain.StatusDeleted || a.DisplayName() != "" || a.Bio() != "" || a.AvatarKey() != "" ||
		a.DeletedAt() == nil || !a.DeletedAt().Equal(now) {
		t.Fatalf("after delete: %+v", a)
	}
	if changed, err = a.Delete(now.Add(time.Hour)); err != nil || changed {
		t.Fatalf("delete again: %v %v", changed, err)
	}
	if err = a.CanWrite(now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("a deleted account writes: %v", err)
	}
	if err = a.Suspend("spam", nil, now); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("suspend a deleted account: %v", err)
	}
}

func TestDeleteRefusesASuspension(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	if err := a.Suspend("spam", nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Delete(now); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("delete while suspended: %v", err)
	}
	until := now.Add(time.Hour)
	b, _ := domain.New(uuid.NewV7(), "Lan", now)
	if err := b.Suspend("spam", &until, now); err != nil {
		t.Fatal(err)
	}
	if changed, err := b.Delete(until); err != nil || !changed {
		t.Fatalf("delete after the suspension lapsed: %v %v", changed, err)
	}
}

func TestStatusRefusal(t *testing.T) {
	t.Parallel()
	if domain.StatusActive.Refusal() != nil {
		t.Fatal("active refuses")
	}
	if !errors.Is(domain.StatusSuspended.Refusal(), domain.ErrSuspended) {
		t.Fatal("suspended")
	}
	if !errors.Is(domain.StatusDeleted.Refusal(), domain.ErrSessionRevoked) {
		t.Fatal("deleted")
	}
}

// Away mode holds from now until its end, then lapses by itself.
func TestAwayLapses(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	if a.Away(now) || a.AwayUntil(now) != nil {
		t.Fatal("a new account is away")
	}
	until := now.Add(48 * time.Hour)
	later := now.Add(time.Minute)
	if err := a.SetAway(until, later); err != nil {
		t.Fatal(err)
	}
	if !a.Away(later) || !a.AwayUntil(later).Equal(until) || !a.UpdatedAt().Equal(later) {
		t.Fatalf("%v %v", a.AwayUntil(later), a.UpdatedAt())
	}
	if a.Away(until) || a.AwayUntil(until) != nil || a.StoredAwayUntil() == nil {
		t.Fatal("away at its end")
	}
	a.ClearAway(later)
	if a.Away(later) || a.StoredAwayUntil() != nil {
		t.Fatal("still away after ClearAway")
	}
}

// The end lies after now and at most 30 days ahead.
func TestSetAwayRefusesAnEndOutsideTheWindow(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	for name, until := range map[string]time.Time{
		"now":     now,
		"past":    now.Add(-time.Hour),
		"too far": now.Add(domain.MaxAway + time.Second),
	} {
		err := a.SetAway(until, now)
		if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidAccount) || e.Fields["away_until"] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if a.StoredAwayUntil() != nil {
		t.Fatal("a refused end was kept")
	}
	if err := a.SetAway(now.Add(domain.MaxAway), now); err != nil {
		t.Fatalf("30 days: %v", err)
	}
}

func TestDeleteEndsAway(t *testing.T) {
	t.Parallel()
	a, _ := domain.New(uuid.NewV7(), "Linh", now)
	if err := a.SetAway(now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Delete(now); err != nil || a.StoredAwayUntil() != nil {
		t.Fatalf("%v %v", err, a.StoredAwayUntil())
	}
}
