package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgUploads struct{ db *pgxpool.Pool }

func NewUploads(db *pgxpool.Pool) Uploads { return pgUploads{db} }

const uploadColumns = `id, owner_id, coalesce(holder_id, '00000000-0000-0000-0000-000000000000'), coalesce(shape, ''), content_type, size, status, coalesce(photo_key, ''), coalesce(thumb_key, ''),
       coalesce(failure, ''), write_id, created_at, updated_at, origin, expires_at`

func scanUpload(row pgx.CollectableRow) (domain.Upload, error) {
	var st domain.UploadState
	if err := row.Scan(&st.ID, &st.OwnerID, &st.HolderID, &st.Shape, &st.ContentType, &st.Size, &st.Status, &st.PhotoKey, &st.ThumbKey,
		&st.Failure, &st.WriteID, &st.CreatedAt, &st.UpdatedAt, &st.Origin, &st.ExpiresAt); err != nil {
		return domain.Upload{}, fmt.Errorf("scan upload: %w", err)
	}
	return domain.RestoreUpload(st), nil
}

func (s pgUploads) Create(ctx context.Context, u domain.Upload) error {
	st := u.State()
	_, err := s.db.Exec(ctx, `
INSERT INTO uploads (id, owner_id, content_type, size, origin, status, write_id, created_at, updated_at, expires_at)
VALUES (@id, @owner_id, @content_type, @size, @origin, @status, @write_id, @created_at, @updated_at, @expires_at)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
		"id": st.ID, "owner_id": st.OwnerID, "content_type": st.ContentType, "size": st.Size, "origin": st.Origin,
		"status": st.Status, "write_id": st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		"expires_at": st.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert upload: %w", err)
	}
	return nil
}

func (s pgUploads) Get(ctx context.Context, id uuid.UUID) (domain.Upload, error) {
	rows, err := s.db.Query(ctx, `SELECT `+uploadColumns+` FROM uploads WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return domain.Upload{}, fmt.Errorf("select upload: %w", err)
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUpload)
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
	ups, err := pgx.CollectRows(rows, scanUpload)
	if err != nil {
		return nil, fmt.Errorf("read uploads: %w", err)
	}
	return ups, nil
}

// saveUploads updates every upload of its arrays under its read write ID.
const saveUploads = `
UPDATE uploads u SET status = v.status, holder_id = NULLIF(v.holder_id, '00000000-0000-0000-0000-000000000000'::uuid),
       shape = NULLIF(v.shape, ''), photo_key = NULLIF(v.photo_key, ''), thumb_key = NULLIF(v.thumb_key, ''),
       failure = NULLIF(v.failure, ''), expires_at = v.expires_at, updated_at = v.updated_at, write_id = @write_id
FROM unnest(@ids::uuid[], @read_write_ids::uuid[], @statuses::text[], @holder_ids::uuid[], @shapes::text[], @photo_keys::text[],
    @thumb_keys::text[], @failures::text[], @expires_at::timestamptz[], @updated_at::timestamptz[])
    AS v(id, read_write_id, status, holder_id, shape, photo_key, thumb_key, failure, expires_at, updated_at)
WHERE u.id = v.id AND (u.write_id = v.read_write_id OR u.write_id = @write_id)`

// update saves ups in one statement; an upload changed meanwhile is
// ErrConflict, which q must roll back when ups holds more than one.
func update(ctx context.Context, q postgres.Execer, ups []*domain.Upload, writeID uuid.UUID) error {
	n := len(ups)
	ids, readWriteIDs, holderIDs := make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n)
	statuses, shapes, photoKeys, thumbKeys, failures := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	expiresAt, updatedAt := make([]time.Time, n), make([]time.Time, n)
	for i, u := range ups {
		st := u.State()
		ids[i], readWriteIDs[i], holderIDs[i] = st.ID, st.WriteID, st.HolderID
		statuses[i], shapes[i], photoKeys[i], thumbKeys[i], failures[i] = string(st.Status), string(st.Shape), st.PhotoKey, st.ThumbKey, st.Failure
		expiresAt[i], updatedAt[i] = st.ExpiresAt, st.UpdatedAt
	}
	tag, err := q.Exec(ctx, saveUploads, pgx.StrictNamedArgs{
		"write_id": writeID, "ids": ids, "read_write_ids": readWriteIDs, "statuses": statuses, "holder_ids": holderIDs,
		"shapes": shapes, "photo_keys": photoKeys, "thumb_keys": thumbKeys, "failures": failures, "expires_at": expiresAt,
		"updated_at": updatedAt,
	})
	if err != nil {
		return fmt.Errorf("update uploads: %w", err)
	}
	if tag.RowsAffected() != int64(n) {
		return ErrConflict
	}
	return nil
}

func (s pgUploads) Save(ctx context.Context, u *domain.Upload, writeID uuid.UUID) error {
	if err := update(ctx, s.db, []*domain.Upload{u}, writeID); err != nil {
		return err
	}
	u.Saved(writeID)
	return nil
}

func (s pgUploads) SaveAll(ctx context.Context, ups []*domain.Upload, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return update(ctx, tx, ups, writeID)
	})
	if err != nil {
		return fmt.Errorf("save uploads: %w", err)
	}
	for _, u := range ups {
		u.Saved(writeID)
	}
	return nil
}
