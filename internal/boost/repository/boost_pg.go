package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgStore struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) Store { return pgStore{db} }

const columns = `id, listing_id, seller_id, request_key, package_code, run_days, price, status, payment_id, bumps,
       created_at, started_at, ends_at, next_tick_at, last_bump_at, ended_at, refunded_at, write_id`

// live is the statuses the partial unique index boosts_live holds one of.
const live = `status IN ('awaiting_payment', 'active')`

func scan(row pgx.CollectableRow) (domain.Boost, error) {
	var st domain.State
	if err := row.Scan(&st.ID, &st.ListingID, &st.SellerID, &st.RequestKey, &st.Package, &st.Days, &st.Price, &st.Status,
		&st.PaymentID, &st.Bumps, &st.CreatedAt, &st.StartedAt, &st.EndsAt, &st.NextTickAt, &st.LastBumpAt, &st.EndedAt,
		&st.RefundedAt, &st.WriteID); err != nil {
		return domain.Boost{}, fmt.Errorf("scan boost: %w", err)
	}
	return domain.Restore(st), nil
}

// querier is a pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func read(ctx context.Context, q querier, where string, args pgx.StrictNamedArgs) ([]domain.Boost, error) {
	rows, err := q.Query(ctx, `SELECT `+columns+` FROM boosts `+where, args)
	if err != nil {
		return nil, fmt.Errorf("select boosts: %w", err)
	}
	bs, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("read boosts: %w", err)
	}
	return bs, nil
}

func one(ctx context.Context, q querier, where string, args pgx.StrictNamedArgs) (domain.Boost, error) {
	bs, err := read(ctx, q, where, args)
	if err != nil {
		return domain.Boost{}, err
	}
	if len(bs) == 0 {
		return domain.Boost{}, domain.ErrNotFound
	}
	return bs[0], nil
}

func (s pgStore) Insert(ctx context.Context, b domain.Boost) (domain.Boost, bool, error) {
	st := b.State()
	var (
		out     domain.Boost
		existed bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		lock := &pgx.Batch{}
		// A share lock on the seller: a purchase racing the account's
		// deletion lands before it or sees the tombstone.
		postgres.QueueLockLiving(lock, st.SellerID, accountdomain.ErrSessionRevoked)
		postgres.QueueLock(lock, "boost", st.ListingID)
		if err := tx.SendBatch(ctx, lock).Close(); err != nil {
			return fmt.Errorf("lock listing's boosts: %w", err)
		}
		prior, err := read(ctx, tx, `WHERE seller_id = @seller AND request_key = @key`,
			pgx.StrictNamedArgs{"seller": st.SellerID, "key": st.RequestKey})
		if err != nil {
			return err
		}
		if len(prior) > 0 {
			out, existed = prior[0], true
			return nil
		}
		current, err := read(ctx, tx, `WHERE listing_id = @listing AND `+live, pgx.StrictNamedArgs{"listing": st.ListingID})
		if err != nil {
			return err
		}
		if len(current) > 0 {
			c := current[0]
			switch {
			case c.Status() == domain.StatusActive:
				return domain.ErrActive.WithBoost(c.ID())
			case c.Package() == st.Package:
				out, existed = c, true
				return nil
			}
			c.Cancel(st.CreatedAt)
			if err = save(ctx, tx, &c, uuid.NewV7()); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO boosts (id, listing_id, seller_id, request_key, package_code, run_days, price, status,
    payment_id, bumps, created_at, write_id)
VALUES (@id, @listing, @seller, @key, @package, @days, @price, @status, @payment, 0, @created_at, @write_id)`, pgx.StrictNamedArgs{
			"id": st.ID, "listing": st.ListingID, "seller": st.SellerID, "key": st.RequestKey, "package": st.Package,
			"days": st.Days, "price": st.Price, "status": st.Status, "payment": st.PaymentID, "created_at": st.CreatedAt,
			"write_id": st.WriteID,
		}); err != nil {
			return fmt.Errorf("insert boost: %w", err)
		}
		out = b
		return nil
	})
	if err != nil {
		return domain.Boost{}, false, fmt.Errorf("buy boost: %w", err)
	}
	return out, existed, nil
}

func (s pgStore) ByKey(ctx context.Context, seller, key uuid.UUID) (domain.Boost, error) {
	return one(ctx, s.db, `WHERE seller_id = @seller AND request_key = @key`, pgx.StrictNamedArgs{"seller": seller, "key": key})
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (domain.Boost, error) {
	return one(ctx, s.db, `WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
}

func (s pgStore) ByPayment(ctx context.Context, payment uuid.UUID) (domain.Boost, error) {
	return one(ctx, s.db, `WHERE payment_id = @payment`, pgx.StrictNamedArgs{"payment": payment})
}

func (s pgStore) Live(ctx context.Context, listings []uuid.UUID) (map[uuid.UUID]domain.Boost, error) {
	out := map[uuid.UUID]domain.Boost{}
	if len(listings) == 0 {
		return out, nil
	}
	bs, err := read(ctx, s.db, `WHERE listing_id = ANY(@listings) AND `+live, pgx.StrictNamedArgs{"listings": listings})
	if err != nil {
		return nil, err
	}
	for i := range bs {
		out[bs[i].ListingID()] = bs[i]
	}
	return out, nil
}

func (s pgStore) OfListing(ctx context.Context, listing uuid.UUID, p page.Request) ([]domain.Boost, error) {
	return read(ctx, s.db, `WHERE listing_id = @listing AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"listing": listing, "before": p.Before(), "limit": p.Fetch()})
}

func (s pgStore) Save(ctx context.Context, b *domain.Boost, writeID uuid.UUID, evs ...events.Event) error {
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return save(ctx, tx, b, writeID, evs...) }); err != nil {
		return fmt.Errorf("save boost: %w", err)
	}
	return nil
}

func save(ctx context.Context, tx pgx.Tx, b *domain.Boost, writeID uuid.UUID, evs ...events.Event) error {
	st := b.State()
	batch := &pgx.Batch{}
	batch.Queue(`UPDATE boosts SET status = @status, bumps = @bumps, started_at = @started_at, ends_at = @ends_at,
    next_tick_at = @next_tick_at, last_bump_at = @last_bump_at, ended_at = @ended_at, refunded_at = @refunded_at,
    write_id = @write_id
WHERE id = @id AND write_id = @read_write_id`, pgx.StrictNamedArgs{
		"id": st.ID, "status": st.Status, "bumps": st.Bumps, "started_at": st.StartedAt, "ends_at": st.EndsAt,
		"next_tick_at": st.NextTickAt, "last_bump_at": st.LastBumpAt, "ended_at": st.EndedAt, "refunded_at": st.RefundedAt,
		"write_id": writeID, "read_write_id": st.WriteID,
	}).Exec(func(tag pgconn.CommandTag) error {
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	})
	events.Queue(batch, evs...)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("save boost: %w", err)
	}
	b.Saved(writeID)
	return nil
}
