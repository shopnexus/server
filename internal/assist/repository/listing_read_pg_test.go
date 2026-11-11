package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/assist/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

var link = domain.Link{Site: domain.SiteChotot, ListID: 119482736, URL: "https://www.chotot.com/119482736.htm"}

func transcriptOf(t *testing.T, db *pgxpool.Pool, id uuid.UUID) *string {
	t.Helper()
	var s *string
	if err := db.QueryRow(context.Background(), `SELECT transcript FROM listing_reads WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func waiting(t *testing.T, db *pgxpool.Pool, account uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM listing_read_transcripts WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": account}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestListingReadInsertGetAndNotify(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	for _, r := range []domain.ListingRead{
		domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now),
		domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now),
		domain.NewLinkRead(uuid.NewV7(), me, link, now),
	} {
		if err := reads.Insert(ctx, r, ""); err != nil {
			t.Fatal(err)
		}
		if err := reads.Insert(ctx, r, ""); err != nil { // a retried step
			t.Fatalf("second insert: %v", err)
		}
		got, err := reads.Get(ctx, r.ID())
		if err != nil || got.Source() != r.Source() || got.Media() != r.Media() || got.Link() != r.Link() ||
			got.Status() != domain.StatusReading || got.Photos() != nil || got.Draft() != nil || !got.ExpiresAt().Equal(now.Add(24*time.Hour)) {
			t.Fatalf("%+v %v", got.State(), err)
		}
	}
	p, err := stream.PGLog{DB: db}.Since(ctx, me, 0, 10)
	if err != nil || len(p.Updates) != 3 || p.Updates[0].Kind != stream.KindListingRead || p.Updates[0].ListingReadID == nil {
		t.Fatalf("one update a read: %+v %v", p, err)
	}
	if _, err = reads.Get(ctx, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown read: %v", err)
	}
}

// Each source has its own cap; a full one refuses only its own source.
func TestCapsPerSource(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	for range domain.PhotoCap {
		if err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), ""); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("photo read %d: %v", domain.PhotoCap+1, err)
	}
	if err := reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now), ""); err != nil {
		t.Fatalf("a video read past the photo cap: %v", err)
	}
	// Reads older than the window no longer count.
	later := now.Add(domain.ReadWindow + time.Minute)
	if err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), later), ""); err != nil {
		t.Fatalf("a photo read a day later: %v", err)
	}
}

func TestInsertForADeletedAccount(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	me := accounttest.New(t, db)
	if _, err := db.Exec(context.Background(), `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": me}); err != nil {
		t.Fatal(err)
	}
	err := repository.NewListingReads(db).Insert(context.Background(), domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), "")
	if !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("%v", err)
	}
}

// The waiting transcript moves into its video read once; a later insert
// under the same key finds nothing, and stale waits are pruned.
func TestTranscriptMovesOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	if err := reads.PutTranscript(ctx, me, "key-1", "Xe đạp Asama, một triệu hai.", now); err != nil {
		t.Fatal(err)
	}
	if err := reads.PutTranscript(ctx, me, "stale", "cũ", now.Add(-domain.TranscriptWait-time.Second)); err != nil {
		t.Fatal(err)
	}
	r := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if err := reads.Insert(ctx, r, "key-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := reads.Transcript(ctx, r.ID()); err != nil || got != "Xe đạp Asama, một triệu hai." {
		t.Fatalf("%q %v", got, err)
	}
	if n := waiting(t, db, me); n != 0 {
		t.Fatalf("%d transcripts still waiting", n)
	}
	other := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if err := reads.Insert(ctx, other, "key-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := reads.Transcript(ctx, other.ID()); err != nil || got != "" {
		t.Fatalf("a second read took %q: %v", got, err)
	}
	// A photo read never takes a transcript, even under a key that has one.
	if err := reads.PutTranscript(ctx, me, "key-2", "nghe được", now); err != nil {
		t.Fatal(err)
	}
	p := domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if err := reads.Insert(ctx, p, "key-2"); err != nil {
		t.Fatalf("a photo read under a transcript's key: %v", err)
	}
	if s := transcriptOf(t, db, p.ID()); s != nil {
		t.Fatalf("a photo read holds %q", *s)
	}
}

// Saving an ended read drops its transcript; a save while reading keeps it.
func TestSaveDropsTheTranscriptAtTheEnd(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	if err := reads.PutTranscript(ctx, me, "k", "một triệu hai", now); err != nil {
		t.Fatal(err)
	}
	r := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if err := reads.Insert(ctx, r, "k"); err != nil {
		t.Fatal(err)
	}
	at := 1.5
	if err := r.SetPhotos([]domain.Photo{{UploadID: uuid.NewV7(), AtSeconds: &at}}); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if s := transcriptOf(t, db, r.ID()); s == nil || *s != "một triệu hai" {
		t.Fatalf("a read still reading lost its transcript: %v", s)
	}
	if err := r.Fail(domain.FailNoItem); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if s := transcriptOf(t, db, r.ID()); s != nil {
		t.Fatalf("an ended read holds %q", *s)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.Failure() != domain.FailNoItem || len(got.Photos()) != 1 || *got.Photos()[0].AtSeconds != 1.5 {
		t.Fatalf("%+v %v", got.State(), err)
	}
}

func TestSaveUnderTheWriteID(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	r := domain.NewLinkRead(uuid.NewV7(), me, link, now)
	if err := reads.Insert(ctx, r, ""); err != nil {
		t.Fatal(err)
	}
	stale := r
	if err := r.SetLinkPhotos(3); err != nil {
		t.Fatal(err)
	}
	w := uuid.NewV7()
	if err := reads.Save(ctx, &r, w); err != nil {
		t.Fatal(err)
	}
	retried := stale
	if err := reads.Save(ctx, &retried, w); err != nil { // the same step retried
		t.Fatalf("retried save: %v", err)
	}
	if err := reads.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save from an old copy: %v", err)
	}
	if err := r.SetDraft(domain.Draft{Title: "iPhone 11", Photos: []uuid.UUID{uuid.NewV7()}}, true); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.LinkPhotos() != 3 || !got.ContactRemoved() || got.Draft().Title != "iPhone 11" {
		t.Fatalf("%+v %v", got.State(), err)
	}
}

func TestDeleteAccountsReadsAndTranscripts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	for _, a := range []uuid.UUID{me, other} {
		if err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), a, uuid.NewV7(), now), ""); err != nil {
			t.Fatal(err)
		}
		if err := reads.PutTranscript(ctx, a, "k", "x", now); err != nil {
			t.Fatal(err)
		}
	}
	if err := reads.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	var mine, theirs int
	if err := db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE account_id = @me), count(*) FILTER (WHERE account_id = @other)
FROM listing_reads`, pgx.StrictNamedArgs{"me": me, "other": other}).Scan(&mine, &theirs); err != nil || mine != 0 || theirs != 1 {
		t.Fatalf("reads %d mine, %d theirs: %v", mine, theirs, err)
	}
	if waiting(t, db, me) != 0 || waiting(t, db, other) != 1 {
		t.Fatal("transcripts")
	}
}
