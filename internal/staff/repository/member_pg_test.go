package repository_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

func sealer(t *testing.T) cipher.AEAD {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return aead
}

func newStore(t *testing.T) (repository.Members, *pgxpool.Pool) {
	t.Helper()
	db := pgtest.New(t)
	return repository.NewMembers(db, sealer(t)), db
}

func created(t *testing.T, s repository.Members, m domain.Member) {
	t.Helper()
	e := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.StaffCreate, Target: audit.Staff(m.ID()), At: now}
	if err := s.CreateAudited(context.Background(), m, e); err != nil {
		t.Fatal(err)
	}
}

func actions(t *testing.T, db *pgxpool.Pool, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM staff_actions WHERE target_id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateGetFind(t *testing.T) {
	t.Parallel()
	s, db := newStore(t)
	ctx := context.Background()
	m := newMember(t, domain.RoleModerator)
	created(t, s, m)
	if err := s.CreateAudited(ctx, m, audit.Entry{Actor: audit.ByCLI("test"), Action: audit.StaffCreate, Target: audit.Staff(m.ID()), At: now}); err != nil {
		t.Fatalf("creating the same id again: %v", err)
	}
	if actions(t, db, m.ID()) != 1 {
		t.Fatal("a repeated create wrote a second audit row")
	}
	got, err := s.Get(ctx, m.ID())
	if err != nil || got.Email() != "mod@example.com" || got.Next() != domain.StepChangePassword {
		t.Fatalf("%+v %v", got, err)
	}
	if f, err := s.FindByEmail(ctx, "mod@example.com"); err != nil || f.ID() != m.ID() {
		t.Fatalf("find: %v", err)
	}
	if _, err := s.FindByEmail(ctx, "nobody@example.com"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown email: %v", err)
	}
	if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestEmailTaken(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	created(t, s, newMember(t, domain.RoleModerator))
	other := newMember(t, domain.RoleAdmin)
	err := s.CreateAudited(context.Background(), other, audit.Entry{Actor: audit.ByCLI("test"), Action: audit.StaffCreate, Target: audit.Staff(other.ID()), At: now})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretsAreSealed(t *testing.T) {
	t.Parallel()
	s, db := newStore(t)
	ctx := context.Background()
	m := newMember(t, domain.RoleModerator)
	created(t, s, m)
	secret := domain.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	if err := s.Save(ctx, &m, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT totp_pending_secret FROM staff_members WHERE id = @id`, pgx.StrictNamedArgs{"id": m.ID()}).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, secret) || len(raw) <= len(secret) {
		t.Fatal("the secret is stored in the clear")
	}
	got, _ := s.Get(ctx, m.ID())
	if err := got.AcceptCode(domain.Code(secret, now), now); err != nil {
		t.Fatalf("the sealed secret does not open: %v", err)
	}
}

func TestSaveAuditedConflictWritesNoAudit(t *testing.T) {
	t.Parallel()
	s, db := newStore(t)
	ctx := context.Background()
	m := newMember(t, domain.RoleModerator)
	created(t, s, m)
	stale := m
	m.Disable(now)
	e := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.StaffDisable, Target: audit.Staff(m.ID()), At: now}
	if err := s.SaveAudited(ctx, &m, uuid.NewV7(), e); err != nil {
		t.Fatal(err)
	}
	stale.Enable(now)
	if err := s.SaveAudited(ctx, &stale, uuid.NewV7(), e); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if actions(t, db, m.ID()) != 2 {
		t.Fatalf("rows %d, want the create and the disable", actions(t, db, m.ID()))
	}
}

func TestListNewestFirst(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	first := newMember(t, domain.RoleModerator)
	created(t, s, first)
	second, _ := domain.NewMember(uuid.NewV7(), "two@example.com", "Two", domain.RoleModerator, "x", now)
	created(t, s, second)
	got, err := s.List(context.Background(), page.Request{Limit: 10})
	if err != nil || len(got) != 2 || got[0].ID() != second.ID() {
		t.Fatalf("%v %v", got, err)
	}
}

// Save leaves the member carrying the write ID it wrote, so saving it again
// succeeds.
func TestSaveTwice(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	m := newMember(t, domain.RoleModerator)
	created(t, s, m)
	m.Disable(now)
	if err := s.Save(context.Background(), &m, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	m.Enable(now)
	if err := s.Save(context.Background(), &m, uuid.NewV7()); err != nil {
		t.Fatalf("second save: %v", err)
	}
}
