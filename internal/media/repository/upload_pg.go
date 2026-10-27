package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/media/domain"
)

type pgUploads struct{ db *pgxpool.Pool }

func NewUploads(db *pgxpool.Pool) Uploads { return pgUploads{db} }

const uploadColumns = `id, owner_id, coalesce(holder_id, '00000000-0000-0000-0000-000000000000'), coalesce(shape, ''), content_type, size, status, coalesce(photo_key, ''), coalesce(thumb_key, ''),
       coalesce(failure, ''), write_id, created_at, updated_at`

func scanUpload(row pgx.Row) (domain.Upload, error) {
	var st domain.UploadState
	if err := row.Scan(&st.ID, &st.OwnerID, &st.HolderID, &st.Shape, &st.ContentType, &st.Size, &st.Status, &st.PhotoKey, &st.ThumbKey,
		&st.Failure, &st.WriteID, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return domain.Upload{}, fmt.Errorf("scan upload: %w", err)
	}
	return domain.RestoreUpload(st), nil
}

func (s pgUploads) Create(ctx context.Context, u domain.Upload) error {
	st := u.State()
	_, err := s.db.Exec(ctx, `
INSERT INTO uploads (id, owner_id, content_type, size, status, write_id, created_at, updated_at)
VALUES (@id, @owner_id, @content_type, @size, @status, @write_id, @created_at, @updated_at)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
		"id": st.ID, "owner_id": st.OwnerID, "content_type": st.ContentType, "size": st.Size, "status": st.Status,
		"write_id": st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert upload: %w", err)
	}
	return nil
}

func (s pgUploads) Get(ctx context.Context, id uuid.UUID) (domain.Upload, error) {
	u, err := scanUpload(s.db.QueryRow(ctx, `SELECT `+uploadColumns+` FROM uploads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Upload{}, ErrNotFound
	}
	if err != nil {
		return domain.Upload{}, fmt.Errorf("select upload: %w", err)
	}
	return u, nil
}

func (s pgUploads) GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Upload, error) {
	rows, err := s.db.Query(ctx, `SELECT `+uploadColumns+` FROM uploads WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select uploads: %w", err)
	}
	defer rows.Close()
	var out []domain.Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read uploads: %w", err)
	}
	return out, nil
}

const saveUpload = `
UPDATE uploads SET status = @status, holder_id = NULLIF(@holder_id, '00000000-0000-0000-0000-000000000000'::uuid),
       shape = NULLIF(@shape, ''),
       photo_key = NULLIF(@photo_key, ''), thumb_key = NULLIF(@thumb_key, ''),
       failure = NULLIF(@failure, ''), updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

// execer is a pool or a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func update(ctx context.Context, q execer, u *domain.Upload, writeID uuid.UUID) error {
	st := u.State()
	tag, err := q.Exec(ctx, saveUpload, pgx.StrictNamedArgs{
		"id": st.ID, "read_write_id": st.WriteID, "write_id": writeID, "status": st.Status, "holder_id": st.HolderID,
		"shape": st.Shape, "photo_key": st.PhotoKey, "thumb_key": st.ThumbKey, "failure": st.Failure, "updated_at": st.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("update upload: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgUploads) Save(ctx context.Context, u *domain.Upload, writeID uuid.UUID) error {
	if err := update(ctx, s.db, u, writeID); err != nil {
		return err
	}
	u.Saved(writeID)
	return nil
}

func (s pgUploads) SaveAll(ctx context.Context, ups []*domain.Upload, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for _, u := range ups {
			if err := update(ctx, tx, u, writeID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save uploads: %w", err)
	}
	for _, u := range ups {
		u.Saved(writeID)
	}
	return nil
}
