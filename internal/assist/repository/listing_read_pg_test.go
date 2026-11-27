package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/stream"
)

var now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

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
	} {
		if _, err := reads.Insert(ctx, r, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
		if _, err := reads.Insert(ctx, r, uuid.Nil()); err != nil { // a retried step
			t.Fatalf("second insert: %v", err)
		}
		got, err := reads.Get(ctx, r.ID())
		if err != nil || got.Source() != r.Source() || got.Media() != r.Media() ||
			got.Status() != domain.StatusReading || got.Photos() != nil || got.Draft() != nil || !got.ExpiresAt().Equal(now.Add(24*time.Hour)) {
			t.Fatalf("%+v %v", got.State(), err)
		}
	}
	p, err := stream.PGLog{DB: db}.Since(ctx, me, 0, 10)
	if err != nil || len(p.Updates) != 2 || p.Updates[0].Kind != stream.KindListingRead || p.Updates[0].ListingReadID == nil {
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
		if _, err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.Nil()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.Nil()); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("photo read %d: %v", domain.PhotoCap+1, err)
	}
	if _, err := reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.Nil()); err != nil {
		t.Fatalf("a video read past the photo cap: %v", err)
	}
	// Reads older than the window no longer count.
	later := now.Add(domain.ReadWindow + time.Minute)
	if _, err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), later), uuid.Nil()); err != nil {
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
	_, err := repository.NewListingReads(db).Insert(context.Background(), domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.Nil())
	if !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("%v", err)
	}
}

// The waiting transcript moves into its video read once; a later insert
// under the same key gives back that read, and stale waits are pruned.
func TestTranscriptMovesOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me, key1, key2 := accounttest.New(t, db), uuid.NewV7(), uuid.NewV7()
	if err := reads.PutTranscript(ctx, me, key1.String(), "Xe đạp Asama, một triệu hai.", now); err != nil {
		t.Fatal(err)
	}
	if err := reads.PutTranscript(ctx, me, "stale", "cũ", now.Add(-domain.TranscriptWait-time.Second)); err != nil {
		t.Fatal(err)
	}
	r := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if _, err := reads.Insert(ctx, r, key1); err != nil {
		t.Fatal(err)
	}
	if got, err := reads.Transcript(ctx, r.ID()); err != nil || got != "Xe đạp Asama, một triệu hai." {
		t.Fatalf("%q %v", got, err)
	}
	if n := waiting(t, db, me); n != 0 {
		t.Fatalf("%d transcripts still waiting", n)
	}
	other := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if id, err := reads.Insert(ctx, other, key1); err != nil || id != r.ID() {
		t.Fatalf("a second read under the key: %s %v", id, err)
	}
	if id, err := reads.ByKey(ctx, me, key1); err != nil || id != r.ID() {
		t.Fatalf("by key: %s %v", id, err)
	}
	// A photo read never takes a transcript, even under a key that has one.
	if err := reads.PutTranscript(ctx, me, key2.String(), "nghe được", now); err != nil {
		t.Fatal(err)
	}
	p := domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if _, err := reads.Insert(ctx, p, key2); err != nil {
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
	me, key := accounttest.New(t, db), uuid.NewV7()
	if err := reads.PutTranscript(ctx, me, key.String(), "một triệu hai", now); err != nil {
		t.Fatal(err)
	}
	r := domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now)
	if _, err := reads.Insert(ctx, r, key); err != nil {
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
	if err := r.Fail(domain.FailNoItem, "Video chỉ có một căn phòng."); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if s := transcriptOf(t, db, r.ID()); s != nil {
		t.Fatalf("an ended read holds %q", *s)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.Failure() != domain.FailNoItem || got.FailureReason() != "Video chỉ có một căn phòng." ||
		len(got.Photos()) != 1 || *got.Photos()[0].AtSeconds != 1.5 {
		t.Fatalf("%+v %v", got.State(), err)
	}
}

func TestSaveUnderTheWriteID(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	photo := uuid.NewV7()
	r := domain.NewPhotoRead(uuid.NewV7(), me, photo, now)
	if _, err := reads.Insert(ctx, r, uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	stale := r
	if err := r.SetPhotos([]domain.Photo{{UploadID: photo}}); err != nil {
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
	if p, err := (stream.PGLog{DB: db}).Since(ctx, me, 0, 10); err != nil || len(p.Updates) != 2 {
		t.Fatalf("the insert and one save notify, the retry not: %+v %v", p, err)
	}
	if err := reads.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save from an old copy: %v", err)
	}
	if err := r.SetDraft(domain.Draft{Title: "iPhone 11", Photos: []uuid.UUID{photo}}); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || len(got.Photos()) != 1 || got.Draft().Title != "iPhone 11" {
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
		if _, err := reads.Insert(ctx, domain.NewPhotoRead(uuid.NewV7(), a, uuid.NewV7(), now), uuid.Nil()); err != nil {
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

// An insert refused for the cap still prunes the stale waiting transcripts.
func TestRefusedInsertPrunesStaleTranscripts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewListingReads(db)
	me := accounttest.New(t, db)
	if err := reads.PutTranscript(ctx, me, "stale", "cũ", now.Add(-domain.TranscriptWait-time.Second)); err != nil {
		t.Fatal(err)
	}
	for range domain.VideoCap {
		if _, err := reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	if err := reads.PutTranscript(ctx, me, "stale", "cũ", now.Add(-domain.TranscriptWait-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), now), uuid.NewV7()); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("%v", err)
	}
	if n := waiting(t, db, me); n != 0 {
		t.Fatalf("%d stale transcripts kept by a refused insert", n)
	}
}
