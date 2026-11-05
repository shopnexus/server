package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

// staffSession stores a member of role with one session from now.
func staffSession(t *testing.T, role domain.Role) (domain.Member, domain.Session, *pgxpool.Pool) {
	t.Helper()
	members, db := newStore(t)
	m := newMember(t, role)
	created(t, members, m)
	_, hash := token.NewOpaque("ss_")
	s := domain.Session{
		ID: uuid.NewV7(), StaffID: m.ID(), TokenHash: hash, CredentialsVersion: 1,
		IP: clientIP, UserAgent: "Firefox", CreatedAt: now, ExpiresAt: now.Add(domain.SessionTTL),
	}
	if err := repository.NewSessions(db).Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return m, s, db
}

func ticketFor(t *testing.T, st repository.Dashboards, s domain.Session, app domain.App) []byte {
	t.Helper()
	_, hash := token.NewOpaque("dt_")
	err := st.CreateTicket(context.Background(), domain.DashboardTicket{
		TokenHash: hash, StaffSessionID: s.ID, App: app, CreatedAt: now, ExpiresAt: now.Add(domain.DashboardTicketTTL),
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func newDashboardSession(app domain.App) domain.DashboardSession {
	_, hash := token.NewOpaque("ds_")
	return domain.DashboardSession{TokenHash: hash, App: app, CreatedAt: now}
}

func TestRedeemTakesATicketOnce(t *testing.T) {
	t.Parallel()
	m, s, db := staffSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ctx := context.Background()
	ticket := ticketFor(t, st, s, domain.AppGrafana)

	if _, err := st.Redeem(ctx, ticket, newDashboardSession(domain.AppRestate)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another app's host: %v", err)
	}
	ds := newDashboardSession(domain.AppGrafana)
	got, err := st.Redeem(ctx, ticket, ds)
	if err != nil || got.StaffSessionID != s.ID || !got.ExpiresAt.Equal(s.ExpiresAt) || got.App != domain.AppGrafana {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = st.Redeem(ctx, ticket, newDashboardSession(domain.AppGrafana)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a used ticket: %v", err)
	}
	g, err := st.Live(ctx, ds.TokenHash, domain.AppGrafana, now)
	if err != nil || g.Email != m.Email() || g.Role != domain.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err = st.Live(ctx, ds.TokenHash, domain.AppRestate, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("on another app: %v", err)
	}
}

func TestRedeemRefusesAnExpiredTicket(t *testing.T) {
	t.Parallel()
	_, s, db := staffSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticket := ticketFor(t, st, s, domain.AppGrafana)
	late := newDashboardSession(domain.AppGrafana)
	late.CreatedAt = now.Add(domain.DashboardTicketTTL)
	if _, err := st.Redeem(context.Background(), ticket, late); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("at its expiry: %v", err)
	}
}

// Two callbacks with one ticket open one session.
func TestRedeemRace(t *testing.T) {
	t.Parallel()
	_, s, db := staffSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticket := ticketFor(t, st, s, domain.AppGrafana)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { _, errs[i] = st.Redeem(context.Background(), ticket, newDashboardSession(domain.AppGrafana)) })
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, repository.ErrNotFound):
			t.Fatal(err)
		default:
		}
	}
	if won != 1 {
		t.Fatalf("%d redeems won", won)
	}
}

// A dashboard session follows its staff session: a sign-out, a disable, a
// credentials change and the session's end stop it, and so does deleting the
// staff session, which deletes its tickets and dashboard sessions.
func TestDashboardSessionFollowsTheStaffSession(t *testing.T) {
	t.Parallel()
	m, s, db := staffSession(t, domain.RoleAdmin)
	st := repository.NewDashboards(db)
	ctx := context.Background()
	ds := newDashboardSession(domain.AppRestate)
	if _, err := st.Redeem(ctx, ticketFor(t, st, s, domain.AppRestate), ds); err != nil {
		t.Fatal(err)
	}
	live := func() error {
		_, err := st.Live(ctx, ds.TokenHash, domain.AppRestate, now)
		return err
	}
	if err := live(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, ds.TokenHash, domain.AppRestate, s.ExpiresAt); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("at the staff session's end: %v", err)
	}
	for name, q := range map[string][2]string{
		"disabled":            {`UPDATE staff_members SET status = 'disabled' WHERE id = @id`, `UPDATE staff_members SET status = 'active' WHERE id = @id`},
		"credentials changed": {`UPDATE staff_members SET credentials_version = 2 WHERE id = @id`, `UPDATE staff_members SET credentials_version = 1 WHERE id = @id`},
	} {
		if _, err := db.Exec(ctx, q[0], pgx.StrictNamedArgs{"id": m.ID()}); err != nil {
			t.Fatal(err)
		}
		if err := live(); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := db.Exec(ctx, q[1], pgx.StrictNamedArgs{"id": m.ID()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := live(); err != nil {
		t.Fatal(err)
	}
	pending := ticketFor(t, st, s, domain.AppRestate)
	if err := repository.NewSessions(db).Revoke(ctx, s.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := live(); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("after a sign-out: %v", err)
	}
	if _, err := st.Redeem(ctx, pending, newDashboardSession(domain.AppRestate)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a ticket after a sign-out: %v", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM staff_sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": s.ID}); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `
SELECT (SELECT count(*) FROM staff_dashboard_tickets) + (SELECT count(*) FROM staff_dashboard_sessions)`, pgx.StrictNamedArgs{}).
		Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d rows left: %v", left, err)
	}
}

// A new ticket deletes the expired tickets of its staff session.
func TestCreateTicketDeletesExpiredOnes(t *testing.T) {
	t.Parallel()
	_, s, db := staffSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticketFor(t, st, s, domain.AppGrafana)
	_, hash := token.NewOpaque("dt_")
	later := now.Add(domain.DashboardTicketTTL)
	if err := st.CreateTicket(context.Background(), domain.DashboardTicket{
		TokenHash: hash, StaffSessionID: s.ID, App: domain.AppGrafana, CreatedAt: later, ExpiresAt: later.Add(domain.DashboardTicketTTL),
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM staff_dashboard_tickets`, pgx.StrictNamedArgs{}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d tickets: %v", n, err)
	}
}
