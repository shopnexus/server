package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
)

func TestFirstSignIn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, temp := e.create(t, "first@example.com", domain.RoleModerator)
	if _, err := e.login("first@example.com", temp, "", "", now); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatalf("no new password: %v", err)
	}
	if _, err := e.enroll("first@example.com", temp, now); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatalf("enrolling before the password: %v", err)
	}
	for _, weak := range []string{"short", temp} {
		if _, err := e.login("first@example.com", temp, weak, "", now); !errors.Is(err, domain.ErrPasswordTooWeak) {
			t.Fatalf("%q: %v", weak, err)
		}
	}
	if _, err := e.login("first@example.com", temp, password, "", now); !errors.Is(err, domain.ErrTOTPEnrollmentRequired) {
		t.Fatalf("after the new password: %v", err)
	}
	if _, err := e.login("first@example.com", temp, password, "", now); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("the temporary password after the change: %v", err)
	}
	if _, err := e.login("first@example.com", password, "", "123456", now); !errors.Is(err, domain.ErrTOTPEnrollmentRequired) {
		t.Fatalf("a code before enrollment started: %v", err)
	}
	enr, err := e.enroll("first@example.com", password, now)
	if err != nil {
		t.Fatal(err)
	}
	secret := decodeSecret(t, enr.Secret)
	if _, err = e.login("first@example.com", password, "", "", now); !errors.Is(err, domain.ErrTOTPRequired) {
		t.Fatalf("no code: %v", err)
	}
	in, err := e.login("first@example.com", password, "", domain.Code(secret, now), now)
	if err != nil || in.Me.Email != "first@example.com" || in.Token == "" || !in.ExpiresAt.Equal(now.Add(domain.SessionTTL)) {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err = e.login("first@example.com", password, "", domain.Code(secret, now), now); !errors.Is(err, domain.ErrInvalidTOTPCode) {
		t.Fatalf("the same code twice: %v", err)
	}
	if _, err = e.enroll("first@example.com", password, now); !errors.Is(err, domain.ErrTOTPEnrolled) {
		t.Fatalf("enrolling again: %v", err)
	}
}

func TestLoginRefusesAlike(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, temp := e.create(t, "known@example.com", domain.RoleModerator)
	for name, try := range map[string][2]string{
		"unknown email":  {"nobody@example.com", temp},
		"wrong password": {"known@example.com", temp + "x"},
	} {
		if _, err := e.login(try[0], try[1], "", "", now); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.at(now).DisableMember(ctx, v.ID, audit.ByCLI("root")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.login("known@example.com", temp, "", "", now); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("disabled: %v", err)
	}
}

func TestUnknownEmailLocksLikeAKnownOne(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, temp := e.create(t, "known@example.com", domain.RoleModerator)
	for _, email := range []string{"known@example.com", "nobody@example.com"} {
		for range domain.MaxFailures {
			_, _ = e.login(email, "wrong password", "", "", now)
		}
		if _, err := e.login(email, temp, "", "", now.Add(time.Minute)); !errors.Is(err, domain.ErrLoginLocked) {
			t.Fatalf("%s: %v", email, err)
		}
	}
	if _, err := e.login("known@example.com", temp, "", "", now.Add(domain.LockFor)); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatalf("after the lock: %v", err)
	}
}

func TestLoginNormalisesTheEmail(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, temp := e.create(t, "admin@example.com", domain.RoleAdmin)
	if _, err := e.login(" Admin@Example.COM ", temp, "", "", now); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatal(err)
	}
	for range domain.MaxFailures {
		_, _ = e.login("ADMIN@example.com", "wrong password", "", "", now)
	}
	if _, err := e.login("admin@example.com", temp, "", "", now); !errors.Is(err, domain.ErrLoginLocked) {
		t.Fatalf("another spelling locked a different row: %v", err)
	}
}

// A call with the right password that only lacks a step counts nothing, so
// a slow first sign-in never locks the member out.
func TestRightPasswordDoesNotCount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, temp := e.create(t, "slow@example.com", domain.RoleModerator)
	for range 2 * domain.MaxFailures {
		if _, err := e.login("slow@example.com", temp, "", "", now); !errors.Is(err, domain.ErrPasswordChangeRequired) {
			t.Fatal(err)
		}
	}
}

func TestLoginCountsConcurrentFailures(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			<-start
			_, _ = e.login("race@example.com", "wrong password", "", "", now)
		})
	}
	close(start)
	wg.Wait()
	var failures int
	if err := e.db.QueryRow(context.Background(), `SELECT failures FROM staff_login_locks`).Scan(&failures); err != nil || failures != 4 {
		t.Fatalf("failures %d, want 4: %v", failures, err)
	}
}

func TestWrongCodesLock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	secret, _ := e.enrolled(t, "codes@example.com")
	later := now.Add(time.Minute)
	good := domain.Code(secret, later)
	for range domain.MaxFailures {
		if _, err := e.login("codes@example.com", password, "", wrong(good), later); !errors.Is(err, domain.ErrInvalidTOTPCode) {
			t.Fatalf("a wrong code: %v", err)
		}
	}
	if _, err := e.login("codes@example.com", password, "", good, later); !errors.Is(err, domain.ErrLoginLocked) {
		t.Fatalf("the right code while locked: %v", err)
	}
}

// burst runs n attempts at once and counts the ones that were evaluated
// (want) and the ones the lock refused before any check.
func burst(t *testing.T, n int, attempt func() error, want error) (evaluated, refused int32) {
	t.Helper()
	var ev, rf atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-start
			switch err := attempt(); {
			case errors.Is(err, want):
				ev.Add(1)
			case errors.Is(err, domain.ErrLoginLocked):
				rf.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	return ev.Load(), rf.Load()
}

func TestLoginRefusesBeyondTheBudget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	evaluated, refused := burst(t, 30, func() error {
		_, err := e.login("burst@example.com", "wrong password", "", "", now)
		return err
	}, domain.ErrInvalidCredentials)
	if evaluated > domain.MaxFailures || evaluated+refused != 30 {
		t.Fatalf("evaluated %d, refused %d", evaluated, refused)
	}
	if _, err := e.login("burst@example.com", "wrong password", "", "", now); !errors.Is(err, domain.ErrLoginLocked) {
		t.Fatalf("after the burst: %v", err)
	}
}

func TestWrongCodesBeyondTheBudget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	secret, _ := e.enrolled(t, "codes@example.com")
	later := now.Add(time.Minute)
	bad := wrong(domain.Code(secret, later))
	evaluated, refused := burst(t, 30, func() error {
		_, err := e.login("codes@example.com", password, "", bad, later)
		return err
	}, domain.ErrInvalidTOTPCode)
	if evaluated > domain.MaxFailures || evaluated+refused != 30 {
		t.Fatalf("evaluated %d, refused %d", evaluated, refused)
	}
}
