package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/stream"
)

type pgListingReads struct{ db *pgxpool.Pool }

func NewListingReads(db *pgxpool.Pool) ListingReads { return pgListingReads{db} }

// insertRead takes in the transcript waiting under the read's account and
// request key, for a video read only.
const insertRead = `
INSERT INTO listing_reads (id, account_id, source, media_id, link_site, link_list_id, link_url, link_photos, transcript,
    status, failure, failure_reason, photos, draft, contact_removed, estimate, created_at, expires_at, write_id)
VALUES (@id, @account_id, @source, @media_id, @link_site, @link_list_id, @link_url, @link_photos,
    (SELECT text FROM listing_read_transcripts
     WHERE account_id = @account_id AND request_key = @request_key AND @source::text = 'video'),
    @status, NULLIF(@failure, ''), NULLIF(@failure_reason, ''), @photos, @draft, @contact_removed, @estimate, @created_at, @expires_at, @write_id)`

// errSaved aborts a save whose row is at its write ID already.
var errSaved = errors.New("assist: saved already")

const saveRead = `
UPDATE listing_reads SET status = @status, failure = NULLIF(@failure, ''),
    failure_reason = NULLIF(@failure_reason, ''), link_photos = @link_photos, photos = @photos,
    draft = @draft, contact_removed = @contact_removed, estimate = @estimate, write_id = @write_id,
    transcript = CASE WHEN @status::text = 'reading' THEN transcript END
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
RETURNING old.write_id = @write_id`

// readArgs are a read's columns. A step not reached yet is NULL, which a
// nil []byte or pointer encodes, and so is a column of another source.
func readArgs(st domain.ListingReadState) (pgx.StrictNamedArgs, error) {
	var (
		photos, draft, estimate []byte
		media                   *uuid.UUID
		site, url               *string
		listID                  *int64
		linkPhotos              *int
		removed                 *bool
		err                     error
	)
	if st.Source != domain.SourceLink {
		media = &st.Media
	} else {
		site, listID, url = &st.Link.Site, &st.Link.ListID, &st.Link.URL
		if st.LinkPhotos > 0 {
			linkPhotos = &st.LinkPhotos
		}
		if st.Draft != nil {
			removed = &st.ContactRemoved
		}
	}
	if st.Photos != nil {
		if photos, err = json.Marshal(st.Photos); err != nil {
			return nil, fmt.Errorf("encode photos: %w", err)
		}
	}
	if st.Draft != nil {
		if draft, err = json.Marshal(st.Draft); err != nil {
			return nil, fmt.Errorf("encode draft: %w", err)
		}
	}
	if st.Estimate != nil {
		if estimate, err = json.Marshal(st.Estimate); err != nil {
			return nil, fmt.Errorf("encode estimate: %w", err)
		}
	}
	return pgx.StrictNamedArgs{
		"id": st.ID, "account_id": st.AccountID, "source": st.Source, "media_id": media,
		"link_site": site, "link_list_id": listID, "link_url": url, "link_photos": linkPhotos,
		"status": st.Status, "failure": st.Failure, "failure_reason": st.FailureReason, "photos": photos, "draft": draft, "contact_removed": removed,
		"estimate": estimate, "created_at": st.CreatedAt, "expires_at": st.ExpiresAt, "write_id": st.WriteID,
	}, nil
}

func (s pgListingReads) Insert(ctx context.Context, r domain.ListingRead, requestKey string) error {
	st := r.State()
	a, err := readArgs(st)
	if err != nil {
		return err
	}
	a["request_key"] = requestKey
	// Stale waits go outside the transaction, so a refused insert prunes them too.
	if _, err = s.db.Exec(ctx, `DELETE FROM listing_read_transcripts WHERE created_at < @stale`,
		pgx.StrictNamedArgs{"stale": st.CreatedAt.Add(-domain.TranscriptWait)}); err != nil {
		return fmt.Errorf("delete stale transcripts: %w", err)
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			stored bool
			n      int
		)
		b := &pgx.Batch{}
		// A share lock on the account's row: an insert that commits after
		// Erase waits for it and sees the tombstone.
		postgres.QueueLockLiving(b, st.AccountID, accountdomain.ErrSessionRevoked)
		b.Queue(`SELECT pg_advisory_xact_lock(hashtext('listing_reads:' || @source || ':' || @account::text))`,
			pgx.StrictNamedArgs{"source": string(st.Source), "account": st.AccountID})
		b.Queue(`SELECT EXISTS (SELECT 1 FROM listing_reads WHERE id = @id),
       (SELECT count(*) FROM listing_reads WHERE account_id = @account AND source = @source AND created_at > @since)`,
			pgx.StrictNamedArgs{"id": st.ID, "account": st.AccountID, "source": string(st.Source), "since": st.CreatedAt.Add(-domain.ReadWindow)}).
			QueryRow(func(row pgx.Row) error {
				if scanErr := row.Scan(&stored, &n); scanErr != nil {
					return fmt.Errorf("count reads: %w", scanErr)
				}
				return nil
			})
		if txErr := tx.SendBatch(ctx, b).Close(); txErr != nil {
			return txErr //nolint:wrapcheck // wrapped below
		}
		if stored {
			return nil
		}
		if n >= st.Source.Cap() {
			return domain.ErrReadLimit
		}
		id := st.ID
		b = &pgx.Batch{}
		b.Queue(insertRead, a)
		b.Queue(`DELETE FROM listing_read_transcripts WHERE account_id = @account AND request_key = @key`,
			pgx.StrictNamedArgs{"account": st.AccountID, "key": requestKey})
		stream.Queue(b, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindListingRead, ListingReadID: &id})
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("insert listing read: %w", err)
	}
	return nil
}

func (s pgListingReads) Save(ctx context.Context, r *domain.ListingRead, writeID uuid.UUID) error {
	st := r.State()
	a, err := readArgs(st)
	if err != nil {
		return err
	}
	for _, k := range []string{"account_id", "source", "media_id", "link_site", "link_list_id", "link_url", "created_at", "expires_at"} {
		delete(a, k)
	}
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	id := st.ID
	b := &pgx.Batch{}
	b.Queue(saveRead, a).QueryRow(func(row pgx.Row) error {
		var saved bool
		scanErr := row.Scan(&saved)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrConflict
		}
		if scanErr != nil {
			return fmt.Errorf("update read: %w", scanErr)
		}
		if saved {
			return errSaved
		}
		return nil
	})
	stream.Queue(b, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindListingRead, ListingReadID: &id})
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	// A retried step finds its row at writeID, written from the same journaled
	// read: the rollback drops only its second notice.
	if err != nil && !errors.Is(err, errSaved) {
		return fmt.Errorf("save listing read: %w", err)
	}
	r.Saved(writeID)
	return nil
}

func (s pgListingReads) Get(ctx context.Context, id uuid.UUID) (domain.ListingRead, error) {
	var (
		st                      domain.ListingReadState
		media                   *uuid.UUID
		site, url               *string
		listID                  *int64
		photos, draft, estimate []byte
	)
	err := s.db.QueryRow(ctx, `SELECT id, account_id, source, media_id, link_site, link_list_id, link_url, coalesce(link_photos, 0),
       status, coalesce(failure, ''), coalesce(failure_reason, ''), photos, draft, coalesce(contact_removed, false), estimate, created_at, expires_at, write_id
FROM listing_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).
		Scan(&st.ID, &st.AccountID, &st.Source, &media, &site, &listID, &url, &st.LinkPhotos,
			&st.Status, &st.Failure, &st.FailureReason, &photos, &draft, &st.ContactRemoved, &estimate, &st.CreatedAt, &st.ExpiresAt, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ListingRead{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ListingRead{}, fmt.Errorf("select listing read: %w", err)
	}
	if media != nil {
		st.Media = *media
	}
	if site != nil {
		st.Link = domain.Link{Site: *site, ListID: *listID, URL: *url}
	}
	for _, col := range []struct {
		raw  []byte
		into any
		name string
	}{{photos, &st.Photos, "photos"}, {draft, &st.Draft, "draft"}, {estimate, &st.Estimate, "estimate"}} {
		if col.raw == nil {
			continue
		}
		if err = json.Unmarshal(col.raw, col.into); err != nil {
			return domain.ListingRead{}, fmt.Errorf("decode %s: %w", col.name, err)
		}
	}
	return domain.RestoreListingRead(st), nil
}

func (s pgListingReads) Transcript(ctx context.Context, id uuid.UUID) (string, error) {
	var text string
	err := s.db.QueryRow(ctx, `SELECT coalesce(transcript, '') FROM listing_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("select transcript: %w", err)
	}
	return text, nil
}

func (s pgListingReads) PutTranscript(ctx context.Context, account uuid.UUID, requestKey, text string, at time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO listing_read_transcripts (account_id, request_key, text, created_at)
VALUES (@account, @key, @text, @at)
ON CONFLICT (account_id, request_key) DO UPDATE SET text = excluded.text, created_at = excluded.created_at`,
		pgx.StrictNamedArgs{"account": account, "key": requestKey, "text": text, "at": at}); err != nil {
		return fmt.Errorf("keep transcript: %w", err)
	}
	return nil
}

func (s pgListingReads) DeleteTranscript(ctx context.Context, account uuid.UUID, requestKey string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listing_read_transcripts WHERE account_id = @account AND request_key = @key`,
		pgx.StrictNamedArgs{"account": account, "key": requestKey}); err != nil {
		return fmt.Errorf("delete waiting transcript: %w", err)
	}
	return nil
}

func (s pgListingReads) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listing_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete listing read: %w", err)
	}
	return nil
}

func (s pgListingReads) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM listing_reads WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account})
	b.Queue(`DELETE FROM listing_read_transcripts WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account})
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("delete the account's reads: %w", err)
	}
	return nil
}
