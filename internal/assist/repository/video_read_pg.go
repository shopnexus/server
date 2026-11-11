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

type pgVideoReads struct{ db *pgxpool.Pool }

func NewVideoReads(db *pgxpool.Pool) VideoReads { return pgVideoReads{db} }

const insertVideoRead = `
INSERT INTO video_reads (id, account_id, video_id, status, failure, frames, draft, estimate, created_at, expires_at, write_id)
VALUES (@id, @account_id, @video_id, @status, NULLIF(@failure, ''), @frames, @draft, @estimate, @created_at, @expires_at, @write_id)`

const saveVideoRead = `
UPDATE video_reads SET status = @status, failure = NULLIF(@failure, ''), frames = @frames, draft = @draft,
    estimate = @estimate, write_id = @write_id
WHERE id = @id AND write_id = @read_write_id`

// args are a read's columns; a step not reached yet is NULL, which a nil
// []byte encodes.
func args(st domain.VideoReadState) (pgx.StrictNamedArgs, error) {
	var frames, draft, estimate []byte
	var err error
	if st.Frames != nil {
		if frames, err = json.Marshal(st.Frames); err != nil {
			return nil, fmt.Errorf("encode frames: %w", err)
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
		"id": st.ID, "account_id": st.AccountID, "video_id": st.VideoID, "status": st.Status, "failure": st.Failure,
		"frames": frames, "draft": draft, "estimate": estimate,
		"created_at": st.CreatedAt, "expires_at": st.ExpiresAt, "write_id": st.WriteID,
	}, nil
}

func (s pgVideoReads) Insert(ctx context.Context, r domain.VideoRead) error {
	st := r.State()
	a, err := args(st)
	if err != nil {
		return err
	}
	err = insertCapped(ctx, s.db, "video_reads", st.ID, st.AccountID, st.CreatedAt, func(tx pgx.Tx) error {
		if _, txErr := tx.Exec(ctx, insertVideoRead, a); txErr != nil {
			return fmt.Errorf("insert read: %w", txErr)
		}
		id := st.ID
		return stream.Notify(ctx, tx, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindVideoRead, VideoReadID: &id})
	})
	if err != nil {
		return fmt.Errorf("insert video read: %w", err)
	}
	return nil
}

func (s pgVideoReads) Save(ctx context.Context, r *domain.VideoRead, writeID uuid.UUID) error {
	st := r.State()
	a, err := args(st)
	if err != nil {
		return err
	}
	for _, k := range []string{"account_id", "video_id", "created_at", "expires_at"} {
		delete(a, k)
	}
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, txErr := tx.Exec(ctx, saveVideoRead, a)
		if txErr != nil {
			return fmt.Errorf("update read: %w", txErr)
		}
		if tag.RowsAffected() == 0 {
			// A retried step finds its own write; anything else is a race.
			var saved bool
			if txErr = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM video_reads WHERE id = @id AND write_id = @write_id)`,
				pgx.StrictNamedArgs{"id": st.ID, "write_id": writeID}).Scan(&saved); txErr != nil {
				return fmt.Errorf("check read: %w", txErr)
			}
			if saved {
				return nil
			}
			return ErrConflict
		}
		id := st.ID
		return stream.Notify(ctx, tx, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindVideoRead, VideoReadID: &id})
	})
	if err != nil {
		return fmt.Errorf("save video read: %w", err)
	}
	r.Saved(writeID)
	return nil
}

func (s pgVideoReads) Get(ctx context.Context, id uuid.UUID) (domain.VideoRead, error) {
	var (
		st                      domain.VideoReadState
		frames, draft, estimate []byte
	)
	err := s.db.QueryRow(ctx, `SELECT id, account_id, video_id, status, coalesce(failure, ''), frames, draft, estimate,
       created_at, expires_at, write_id FROM video_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).
		Scan(&st.ID, &st.AccountID, &st.VideoID, &st.Status, &st.Failure, &frames, &draft, &estimate,
			&st.CreatedAt, &st.ExpiresAt, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.VideoRead{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.VideoRead{}, fmt.Errorf("select video read: %w", err)
	}
	if frames != nil {
		if err = json.Unmarshal(frames, &st.Frames); err != nil {
			return domain.VideoRead{}, fmt.Errorf("decode frames: %w", err)
		}
	}
	if draft != nil {
		if err = json.Unmarshal(draft, &st.Draft); err != nil {
			return domain.VideoRead{}, fmt.Errorf("decode draft: %w", err)
		}
	}
	if estimate != nil {
		if err = json.Unmarshal(estimate, &st.Estimate); err != nil {
			return domain.VideoRead{}, fmt.Errorf("decode estimate: %w", err)
		}
	}
	return domain.RestoreVideoRead(st), nil
}

func (s pgVideoReads) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM video_reads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete video read: %w", err)
	}
	return nil
}

func (s pgVideoReads) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM video_reads WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete the account's video reads: %w", err)
	}
	return nil
}
