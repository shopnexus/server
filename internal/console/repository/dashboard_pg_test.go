package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/token"
)

// consoleSession stores a member of role with one session from now.
func consoleSession(t *testing.T, role domain.Role) (domain.Member, domain.Session, *pgxpool.Pool) {
	t.Helper()
	members, db := newStore(t)
	m := newMember(t, role)
	created(t, members, m)
	_, hash := token.NewOpaque("ss_")
	s := domain.Session{
		ID: uuid.NewV7(), MemberID: m.ID(), TokenHash: hash, CredentialsVersion: 1,
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
		TokenHash: hash, SessionID: s.ID, App: app, CreatedAt: now, ExpiresAt: now.Add(domain.DashboardTicketTTL),
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// cookieHash is the hash of a fresh dashboard cookie's token.
func cookieHash() []byte {
	_, hash := token.NewOpaque("ds_")
	return hash
}

func TestRedeemTakesATicketOnce(t *testing.T) {
	t.Parallel()
	m, s, db := consoleSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ctx := context.Background()
	ticket := ticketFor(t, st, s, domain.AppGrafana)

	if _, err := st.Redeem(ctx, ticket, cookieHash(), domain.AppRestate, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another app's host: %v", err)
	}
	hash := cookieHash()
	expiresAt, err := st.Redeem(ctx, ticket, hash, domain.AppGrafana, now)
	if err != nil || !expiresAt.Equal(s.ExpiresAt) {
		t.Fatalf("%v %v", expiresAt, err)
	}
	if _, err = st.Redeem(ctx, ticket, cookieHash(), domain.AppGrafana, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a used ticket: %v", err)
	}
	g, err := st.Live(ctx, hash, domain.AppGrafana, now)
	if err != nil || g.Email != m.Email() || g.Role != domain.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err = st.Live(ctx, hash, domain.AppRestate, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("on another app: %v", err)
	}
}

func TestRedeemRefusesAnExpiredTicket(t *testing.T) {
	t.Parallel()
	_, s, db := consoleSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticket := ticketFor(t, st, s, domain.AppGrafana)
	late := now.Add(domain.DashboardTicketTTL)
	if _, err := st.Redeem(context.Background(), ticket, cookieHash(), domain.AppGrafana, late); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("at its expiry: %v", err)
	}
}

// Two callbacks with one ticket open one session.
func TestRedeemRace(t *testing.T) {
	t.Parallel()
	_, s, db := consoleSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticket := ticketFor(t, st, s, domain.AppGrafana)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { _, errs[i] = st.Redeem(context.Background(), ticket, cookieHash(), domain.AppGrafana, now) })
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

// A dashboard session follows its console session: a sign-out, a disable, a
// credentials change and the session's end stop it, and so does deleting the
// console session, which deletes its tickets and dashboard sessions.
func TestDashboardSessionFollowsTheConsoleSession(t *testing.T) {
	t.Parallel()
	m, s, db := consoleSession(t, domain.RoleAdmin)
	st := repository.NewDashboards(db)
	ctx := context.Background()
	hash := cookieHash()
	if _, err := st.Redeem(ctx, ticketFor(t, st, s, domain.AppRestate), hash, domain.AppRestate, now); err != nil {
		t.Fatal(err)
	}
	live := func() error {
		_, err := st.Live(ctx, hash, domain.AppRestate, now)
		return err
	}
	if err := live(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, hash, domain.AppRestate, s.ExpiresAt); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("at the console session's end: %v", err)
	}
	for name, q := range map[string][2]string{
		"disabled":            {`UPDATE console_members SET status = 'disabled' WHERE id = @id`, `UPDATE console_members SET status = 'active' WHERE id = @id`},
		"credentials changed": {`UPDATE console_members SET credentials_version = 2 WHERE id = @id`, `UPDATE console_members SET credentials_version = 1 WHERE id = @id`},
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
	if _, err := st.Redeem(ctx, pending, cookieHash(), domain.AppRestate, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a ticket after a sign-out: %v", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM console_sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": s.ID}); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `
SELECT (SELECT count(*) FROM console_dashboard_tickets) + (SELECT count(*) FROM console_dashboard_sessions)`, pgx.StrictNamedArgs{}).
		Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d rows left: %v", left, err)
	}
}

// A new ticket deletes the expired tickets of its console session.
func TestCreateTicketDeletesExpiredOnes(t *testing.T) {
	t.Parallel()
	_, s, db := consoleSession(t, domain.RoleModerator)
	st := repository.NewDashboards(db)
	ticketFor(t, st, s, domain.AppGrafana)
	_, hash := token.NewOpaque("dt_")
	later := now.Add(domain.DashboardTicketTTL)
	if err := st.CreateTicket(context.Background(), domain.DashboardTicket{
		TokenHash: hash, SessionID: s.ID, App: domain.AppGrafana, CreatedAt: later, ExpiresAt: later.Add(domain.DashboardTicketTTL),
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM console_dashboard_tickets`, pgx.StrictNamedArgs{}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d tickets: %v", n, err)
	}
}

// Every app DASHBOARD_HOSTS may name gets a ticket and a session: the
// tables' CHECK lists them all.
func TestEveryDashboardAppIsStored(t *testing.T) {
	t.Parallel()
	_, s, db := consoleSession(t, domain.RoleAdmin)
	st := repository.NewDashboards(db)
	for _, name := range config.DashboardApps {
		app := domain.App(name)
		if _, err := st.Redeem(context.Background(), ticketFor(t, st, s, app), cookieHash(), app, now); err != nil {
			t.Errorf("%s: %v", app, err)
		}
	}
}

// SetFloors writes every floor and its entries, or none when an app is unknown.
func TestSetFloorsAllOrNone(t *testing.T) {
	t.Parallel()
	m, _, db := consoleSession(t, domain.RoleAdmin)
	st := repository.NewDashboards(db)
	ctx := context.Background()
	entry := func(app domain.App) audit.Entry {
		return audit.Entry{Actor: audit.ByCLI("test"), Action: audit.DashboardFloor, Target: audit.Dashboard(string(app)), At: now}
	}
	floors := map[domain.App]domain.Role{domain.AppGrafana: domain.RoleAdmin, "nope": domain.RoleAdmin}
	err := st.SetFloors(ctx, floors, m.ID(), now, []audit.Entry{entry(domain.AppGrafana), entry("nope")})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown app: %v", err)
	}
	if f, _ := st.Floor(ctx, domain.AppGrafana); f != domain.RoleModerator || actions(t, db, audit.Dashboard("grafana").ID) != 0 {
		t.Fatalf("a refused change wrote: floor %s", f)
	}
	floors = map[domain.App]domain.Role{domain.AppGrafana: domain.RoleAdmin, domain.AppRestate: domain.RoleModerator}
	if err = st.SetFloors(ctx, floors, m.ID(), now, []audit.Entry{entry(domain.AppGrafana), entry(domain.AppRestate)}); err != nil {
		t.Fatal(err)
	}
	g, _ := st.Floor(ctx, domain.AppGrafana)
	r, _ := st.Floor(ctx, domain.AppRestate)
	if g != domain.RoleAdmin || r != domain.RoleModerator || actions(t, db, audit.Dashboard("restate").ID) != 1 {
		t.Fatalf("floors %s %s", g, r)
	}
}
