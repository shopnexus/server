package staff_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/quota"
)

func TestFirstSignInSteps(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, temp := e.create(t, "first@example.com", member.RoleModerator)
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "first@example.com", temp, clientIP, now)
	if err != nil || step.Next != member.StepChangePassword || !step.TicketExpiresAt.Equal(now.Add(staff.TicketTTL)) {
		t.Fatalf("%+v %v", step, err)
	}
	if _, err = staff.EnrollTOTP(ctx, e.stores.Members, e.keys, step.Ticket, now); !errors.Is(err, staff.ErrInvalidLoginTicket) {
		t.Fatalf("enrolling before the password: %v", err)
	}
	if _, err = staff.ChangeTemporaryPassword(ctx, e.stores.Members, e.keys, step.Ticket, "short", clientIP, now); !errors.Is(err, member.ErrPasswordTooWeak) {
		t.Fatalf("weak password: %v", err)
	}
	if _, err = staff.ChangeTemporaryPassword(ctx, e.stores.Members, e.keys, step.Ticket, temp, clientIP, now); !errors.Is(err, member.ErrPasswordTooWeak) {
		t.Fatalf("the temporary password again: %v", err)
	}
	next, err := staff.ChangeTemporaryPassword(ctx, e.stores.Members, e.keys, step.Ticket, "a long staff password", clientIP, now)
	if err != nil || next.Next != member.StepEnrollTOTP {
		t.Fatalf("%+v %v", next, err)
	}
	if _, err = staff.EnrollTOTP(ctx, e.stores.Members, e.keys, step.Ticket, now); !errors.Is(err, staff.ErrInvalidLoginTicket) {
		t.Fatalf("the ticket from before the change: %v", err)
	}
	if _, err = staff.VerifyTOTP(ctx, e.stores, e.keys, next.Ticket, "123456", clientIP, "test", now); !errors.Is(err, staff.ErrInvalidLoginTicket) {
		t.Fatalf("a code before enrollment started: %v", err)
	}
	enr, err := staff.EnrollTOTP(ctx, e.stores.Members, e.keys, next.Ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	secret := decodeSecret(t, enr.Secret)
	out, err := staff.VerifyTOTP(ctx, e.stores, e.keys, next.Ticket, member.Code(secret, now), clientIP, "test", now)
	if err != nil || out.SignIn.Staff.Email != "first@example.com" || out.SignIn.TokenType != "Bearer" ||
		!out.SignIn.AccessTokenExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("%+v %v", out, err)
	}
	claims, err := e.keys.Verifier.Verify(out.SignIn.AccessToken, staff.AudienceStaff, now)
	if err != nil || claims.SessionID == (uuid.UUID{}) {
		t.Fatalf("access token: %+v %v", claims, err)
	}
	if _, err := staff.VerifyTOTP(ctx, e.stores, e.keys, next.Ticket, member.Code(secret, now), clientIP, "test", now); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("the same code twice: %v", err)
	}
}

func TestLoginRefusesAlike(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, temp := e.create(t, "known@example.com", member.RoleModerator)
	for name, try := range map[string][2]string{
		"unknown email":  {"nobody@example.com", temp},
		"wrong password": {"known@example.com", temp + "x"},
	} {
		if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, try[0], try[1], clientIP, now); !errors.Is(err, member.ErrInvalidCredentials) {
			t.Errorf("%s: %v", name, err)
		}
	}
	m, _ := e.stores.Members.Get(ctx, v.ID)
	m.Disable(now)
	if err := e.stores.Members.Save(ctx, &m, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "known@example.com", temp, clientIP, now); !errors.Is(err, member.ErrInvalidCredentials) {
		t.Fatalf("disabled: %v", err)
	}
}

func TestUnknownEmailLocksLikeAKnownOne(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, temp := e.create(t, "known@example.com", member.RoleModerator)
	for _, email := range []string{"known@example.com", "nobody@example.com"} {
		for range quota.MaxFailures {
			_, _ = staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, email, "wrong password", clientIP, now)
		}
		_, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, email, temp, clientIP, now.Add(time.Minute))
		if !errors.Is(err, staff.ErrLoginLocked) {
			t.Fatalf("%s: %v", email, err)
		}
	}
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "known@example.com", temp, clientIP, now.Add(quota.LockFor)); err != nil {
		t.Fatalf("after the lock: %v", err)
	}
}

func TestLoginNormalisesTheEmail(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, temp := e.create(t, "admin@example.com", member.RoleAdmin)
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, " Admin@Example.COM ", temp, clientIP, now); err != nil {
		t.Fatal(err)
	}
	for range quota.MaxFailures {
		_, _ = staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "ADMIN@example.com", "wrong password", clientIP, now)
	}
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "admin@example.com", temp, clientIP, now); !errors.Is(err, staff.ErrLoginLocked) {
		t.Fatalf("another spelling locked a different row: %v", err)
	}
}

func TestLoginCountsConcurrentFailures(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			<-start
			_, _ = staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "race@example.com", "wrong password", clientIP, now)
		})
	}
	close(start)
	wg.Wait()
	q, err := e.stores.Quotas.Get(ctx, quota.Key(e.keys.Quota, "race@example.com"))
	if err != nil || q.Failures() != 4 {
		t.Fatalf("failures %d, want 4: %v", q.Failures(), err)
	}
}

func TestWrongCodesLock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	password, secret, _ := e.enrolled(t, "codes@example.com")
	later := now.Add(time.Minute)
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "codes@example.com", password, clientIP, later)
	if err != nil || step.Next != member.StepTOTP {
		t.Fatalf("%+v %v", step, err)
	}
	good := member.Code(secret, later)
	bad := good[:5] + string(rune('0'+(good[5]-'0'+1)%10))
	for range quota.MaxFailures {
		if _, err := staff.VerifyTOTP(ctx, e.stores, e.keys, step.Ticket, bad, clientIP, "test", later); !errors.Is(err, member.ErrInvalidTOTPCode) {
			t.Fatalf("a wrong code: %v", err)
		}
	}
	if _, err := staff.VerifyTOTP(ctx, e.stores, e.keys, step.Ticket, good, clientIP, "test", later); !errors.Is(err, staff.ErrLoginLocked) {
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
			case errors.Is(err, staff.ErrLoginLocked):
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
	ctx := context.Background()
	evaluated, refused := burst(t, 30, func() error {
		_, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "burst@example.com", "wrong password", clientIP, now)
		return err
	}, member.ErrInvalidCredentials)
	if evaluated > quota.MaxFailures || evaluated+refused != 30 {
		t.Fatalf("evaluated %d, refused %d", evaluated, refused)
	}
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "burst@example.com", "wrong password", clientIP, now); !errors.Is(err, staff.ErrLoginLocked) {
		t.Fatalf("after the burst: %v", err)
	}
}

func TestWrongCodesBeyondTheBudget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	password, secret, _ := e.enrolled(t, "codes@example.com")
	later := now.Add(time.Minute)
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "codes@example.com", password, clientIP, later)
	if err != nil {
		t.Fatal(err)
	}
	good := member.Code(secret, later)
	bad := good[:5] + string(rune('0'+(good[5]-'0'+1)%10))
	evaluated, refused := burst(t, 30, func() error {
		_, err := staff.VerifyTOTP(ctx, e.stores, e.keys, step.Ticket, bad, clientIP, "test", later)
		return err
	}, member.ErrInvalidTOTPCode)
	if evaluated > quota.MaxFailures || evaluated+refused != 30 {
		t.Fatalf("evaluated %d, refused %d", evaluated, refused)
	}
}
