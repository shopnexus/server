package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/platform/stream"
)

type pgLinkReads struct{ db *pgxpool.Pool }

func NewLinkReads(db *pgxpool.Pool) LinkReads { return pgLinkReads{db} }

const insertLinkRead = `
INSERT INTO link_reads (id, account_id, site, list_id, url, status, failure, photo_count, draft, contact_removed, estimate,
    created_at, expires_at, write_id)
VALUES (@id, @account_id, @site, @list_id, @url, @status, NULLIF(@failure, ''), @photo_count, @draft, @contact_removed, @estimate,
    @created_at, @expires_at, @write_id)`

const saveLinkRead = `
UPDATE link_reads SET status = @status, failure = NULLIF(@failure, ''), photo_count = @photo_count, draft = @draft,
    contact_removed = @contact_removed, estimate = @estimate, write_id = @write_id
WHERE id = @id AND write_id = @read_write_id`

// linkArgs are a read's columns; a step not reached yet is NULL.
func linkArgs(st domain.LinkReadState) (pgx.StrictNamedArgs, error) {
	var (
		draft, estimate []byte
		count           *int
		removed         *bool
		err             error
	)
	if st.PhotoCount > 0 {
		count = &st.PhotoCount
	}
	if st.Draft != nil {
		if draft, err = json.Marshal(st.Draft); err != nil {
			return nil, fmt.Errorf("encode draft: %w", err)
		}
		removed = &st.ContactRemoved
	}
	if st.Estimate != nil {
		if estimate, err = json.Marshal(st.Estimate); err != nil {
			return nil, fmt.Errorf("encode estimate: %w", err)
		}
	}
	return pgx.StrictNamedArgs{
		"id": st.ID, "account_id": st.AccountID, "site": st.Link.Site, "list_id": st.Link.ListID, "url": st.Link.URL,
		"status": st.Status, "failure": st.Failure, "photo_count": count, "draft": draft, "contact_removed": removed,
		"estimate": estimate, "created_at": st.CreatedAt, "expires_at": st.ExpiresAt, "write_id": st.WriteID,
	}, nil
}

func (s pgLinkReads) Insert(ctx context.Context, r domain.LinkRead) error {
	st := r.State()
	a, err := linkArgs(st)
	if err != nil {
		return err
	}
	err = insertCapped(ctx, s.db, "link_reads", st.ID, st.AccountID, st.CreatedAt, func(tx pgx.Tx) error {
		if _, txErr := tx.Exec(ctx, insertLinkRead, a); txErr != nil {
			return fmt.Errorf("insert read: %w", txErr)
		}
		id := st.ID
		return stream.Notify(ctx, tx, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindLinkRead, LinkReadID: &id})
	})
	if err != nil {
		return fmt.Errorf("insert link read: %w", err)
	}
	return nil
}

func (s pgLinkReads) Save(ctx context.Context, r *domain.LinkRead, writeID uuid.UUID) error {
	st := r.State()
	a, err := linkArgs(st)
	if err != nil {
		return err
	}
	for _, k := range []string{"account_id", "site", "list_id", "url", "created_at", "expires_at"} {
		delete(a, k)
	}
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, txErr := tx.Exec(ctx, saveLinkRead, a)
		if txErr != nil {
			return fmt.Errorf("update read: %w", txErr)
		}
		if tag.RowsAffected() == 0 {
			// A retried step finds its own write; anything else is a race.
			var saved bool
			if txErr = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM link_reads WHERE id = @id AND write_id = @write_id)`,
				pgx.StrictNamedArgs{"id": st.ID, "write_id": writeID}).Scan(&saved); txErr != nil {
				return fmt.Errorf("check read: %w", txErr)
			}
			if saved {
				return nil
			}
			return ErrConflict
		}
		id := st.ID
		return stream.Notify(ctx, tx, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindLinkRead, LinkReadID: &id})
	})
	if err != nil {
		return fmt.Errorf("save link read: %w", err)
	}
	r.Saved(writeID)
	return nil
}

func (s pgLinkReads) Get(ctx context.Context, id uuid.UUID) (domain.LinkRead, error) {
	var (
		st              domain.LinkReadState
		draft, estimate []byte
	)
	err := s.db.QueryRow(ctx, `SELECT id, account_id, site, list_id, url, status, coalesce(failure, ''), coalesce(photo_count, 0),
       draft, coalesce(contact_removed, false), estimate, created_at, expires_at, write_id FROM link_reads WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).
		Scan(&st.ID, &st.AccountID, &st.Link.Site, &st.Link.ListID, &st.Link.URL, &st.Status, &st.Failure, &st.PhotoCount,
			&draft, &st.ContactRemoved, &estimate, &st.CreatedAt, &st.ExpiresAt, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LinkRead{}, domain.ErrLinkReadNotFound
	}
	if err != nil {
		return domain.LinkRead{}, fmt.Errorf("select link read: %w", err)
	}
	if draft != nil {
		if err = json.Unmarshal(draft, &st.Draft); err != nil {
			return domain.LinkRead{}, fmt.Errorf("decode draft: %w", err)
		}
	}
	if estimate != nil {
		if err = json.Unmarshal(estimate, &st.Estimate); err != nil {
			return domain.LinkRead{}, fmt.Errorf("decode estimate: %w", err)
		}
	}
	return domain.RestoreLinkRead(st), nil
}

func (s pgLinkReads) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM link_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete link read: %w", err)
	}
	return nil
}

func (s pgLinkReads) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM link_reads WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete the account's link reads: %w", err)
	}
	return nil
}
