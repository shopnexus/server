package migrations_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// A video read and a link read of the release before arrive in
// listing_reads whole, unreadable_video as unreadable, and their updates
// read listing_read.
func TestListingReadsTakeTheOldReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0039_listing_reads.sql")); err != nil {
		t.Fatal(err)
	}
	// 0039 leaves accounts as 0038 has it, so the account helper of today fits.
	acc := accounttest.New(t, db)
	video, link, frame, cp := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	args := pgx.NamedArgs{"acc": acc, "video": video, "link": link, "frame": frame.String(), "copy": cp.String()}
	for _, q := range []string{
		`INSERT INTO video_reads (id, account_id, video_id, status, failure, frames, created_at, expires_at, write_id)
		 VALUES (@video, @acc, gen_random_uuid(), 'failed', 'unreadable_video',
		         jsonb_build_array(jsonb_build_object('upload_id', @frame::text, 'at_seconds', 0)),
		         now(), now() + interval '1 day', @video)`,
		`INSERT INTO link_reads (id, account_id, site, list_id, url, status, photo_count, draft, contact_removed,
		     created_at, expires_at, write_id)
		 VALUES (@link, @acc, 'chotot.com', 119482736, 'https://www.chotot.com/119482736.htm', 'reading', 3,
		         jsonb_build_object('title', 'iPhone 11', 'photos', jsonb_build_array(@copy::text)), true,
		         now(), now() + interval '1 day', @link)`,
		`INSERT INTO account_updates (account_id, seq, kind, video_read_id) VALUES (@acc, 1, 'video_read', @video)`,
		`INSERT INTO account_updates (account_id, seq, kind, link_read_id) VALUES (@acc, 2, 'link_read', @link)`,
	} {
		if _, err := db.Exec(ctx, q, args); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0039 on a database with reads: %v", err)
	}

	var (
		source, failure string
		frameAt         float64
		frameID         string
	)
	if err := db.QueryRow(ctx, `SELECT source, failure, photos -> 0 ->> 'upload_id', (photos -> 0 ->> 'at_seconds')::float8
FROM listing_reads WHERE id = @video`, args).Scan(&source, &failure, &frameID, &frameAt); err != nil {
		t.Fatal(err)
	}
	if source != "video" || failure != "unreadable" || frameID != frame.String() || frameAt != 0 {
		t.Fatalf("video read: %s %s %s %v", source, failure, frameID, frameAt)
	}

	var (
		site, copyID string
		photos       int
		removed      bool
	)
	if err := db.QueryRow(ctx, `SELECT source, link_site, link_photos, photos -> 0 ->> 'upload_id', contact_removed
FROM listing_reads WHERE id = @link`, args).Scan(&source, &site, &photos, &copyID, &removed); err != nil {
		t.Fatal(err)
	}
	if source != "link" || site != "chotot.com" || photos != 3 || copyID != cp.String() || !removed {
		t.Fatalf("link read: %s %s %d %s %v", source, site, photos, copyID, removed)
	}

	var kinds, named int
	if err := db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind = 'listing_read'),
       count(*) FILTER (WHERE listing_read_id IN (@video, @link)) FROM account_updates WHERE account_id = @acc`, args).
		Scan(&kinds, &named); err != nil || kinds != 2 || named != 2 {
		t.Fatalf("updates: %d listing_read, %d named: %v", kinds, named, err)
	}
}

// The checks hold each source to its own columns.
func TestListingReadsChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	acc := accounttest.New(t, db)
	heard := "heard"
	for name, c := range map[string]struct {
		source     string
		media      bool
		transcript *string
		status     string
	}{
		"a photo read with a transcript":  {"photo", true, &heard, "reading"},
		"a video read without its video":  {"video", false, nil, "reading"},
		"a link read without its link":    {"link", false, nil, "reading"},
		"an ended read with a transcript": {"video", true, &heard, "done"},
	} {
		var media *uuid.UUID
		if c.media {
			m := uuid.NewV7()
			media = &m
		}
		_, err := db.Exec(ctx, `INSERT INTO listing_reads
    (id, account_id, source, media_id, transcript, status, created_at, expires_at, write_id)
VALUES (gen_random_uuid(), @acc, @source, @media, @transcript, @status, now(), now() + interval '1 day', gen_random_uuid())`,
			pgx.NamedArgs{"acc": acc, "source": c.source, "media": media, "transcript": c.transcript, "status": c.status})
		if err == nil {
			t.Errorf("%s was stored", name)
		}
	}
}
