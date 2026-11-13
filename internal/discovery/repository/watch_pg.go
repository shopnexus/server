package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/platform/events"
)

type pgWatches struct{ db *pgxpool.Pool }

func NewWatches(db *pgxpool.Pool) Watches { return pgWatches{db} }

// watchColumns are a Watch's columns in its field order; NULL filters read
// as zero.
const watchColumns = `id, account_id, query, phrases, coalesce(category, ''), coalesce(price_min, 0),
coalesce(price_max, 0), conditions, offers_only, coalesce(commune_code, ''), coalesce(radius_km, 0), alert,
new_count, last_arrival, created_at`

func collectWatches(rows pgx.Rows) ([]Watch, error) {
	ws, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Watch])
	if err != nil {
		return nil, fmt.Errorf("read watches: %w", err)
	}
	return ws, nil
}

// watchFilters are the columns Put writes from w beyond its key, ID and
// time; NULLIF stores a zero filter as NULL.
const watchFilters = `query = @query, phrases = @phrases, category = NULLIF(@category, ''),
    price_min = NULLIF(@price_min::bigint, 0), price_max = NULLIF(@price_max::bigint, 0), conditions = @conditions,
    offers_only = @offers_only, commune_code = NULLIF(@commune, ''), radius_km = NULLIF(@radius::int, 0)`

func (s pgWatches) Put(ctx context.Context, w Watch) (Watch, bool, error) {
	var (
		out     Watch
		created bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := living(ctx, tx, w.Account)
		if err != nil {
			return err
		}
		if !alive {
			return accountdomain.ErrSessionRevoked
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('watches:' || @account::text))`,
			pgx.StrictNamedArgs{"account": w.Account}); err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		filters := pgx.StrictNamedArgs{
			"account": w.Account, "key": domain.HistoryKey(w.Query), "query": w.Query, "phrases": w.Phrases,
			"category": w.Category, "price_min": w.PriceMin, "price_max": w.PriceMax, "conditions": w.Conditions,
			"offers_only": w.OffersOnly, "commune": w.Commune, "radius": w.RadiusKm,
		}
		rows, err := tx.Query(ctx, `UPDATE search_watches SET `+watchFilters+`, new_count = 0
WHERE account_id = @account AND query_key = @key
RETURNING `+watchColumns, filters)
		if err != nil {
			return fmt.Errorf("update watch: %w", err)
		}
		updated, err := collectWatches(rows)
		if err != nil {
			return err
		}
		if len(updated) == 1 {
			out = updated[0]
			return nil
		}
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM search_watches WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": w.Account}).Scan(&n); err != nil {
			return fmt.Errorf("count watches: %w", err)
		}
		if n >= domain.MaxWatches {
			return domain.ErrWatchLimit
		}
		filters["id"], filters["at"] = w.ID, w.CreatedAt
		rows, err = tx.Query(ctx, `INSERT INTO search_watches (id, account_id, query_key, query, phrases, category,
    price_min, price_max, conditions, offers_only, commune_code, radius_km, alert, new_count, last_arrival, created_at)
VALUES (@id, @account, @key, @query, @phrases, NULLIF(@category, ''), NULLIF(@price_min::bigint, 0),
    NULLIF(@price_max::bigint, 0), @conditions, @offers_only, NULLIF(@commune, ''), NULLIF(@radius::int, 0), true, 0,
    coalesce((SELECT max(seq) FROM listing_arrivals), 0), @at)
RETURNING `+watchColumns, filters)
		if err != nil {
			return fmt.Errorf("insert watch: %w", err)
		}
		inserted, err := collectWatches(rows)
		if err != nil {
			return err
		}
		out, created = inserted[0], true
		return nil
	})
	if err != nil {
		return Watch{}, false, fmt.Errorf("put watch: %w", err)
	}
	return out, created, nil
}

func (s pgWatches) Get(ctx context.Context, id uuid.UUID) (Watch, bool, error) {
	rows, err := s.db.Query(ctx, `SELECT `+watchColumns+` FROM search_watches WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Watch{}, false, fmt.Errorf("select watch: %w", err)
	}
	ws, err := collectWatches(rows)
	if err != nil || len(ws) == 0 {
		return Watch{}, false, err
	}
	return ws[0], true, nil
}

func (s pgWatches) List(ctx context.Context, account uuid.UUID) ([]Watch, error) {
	rows, err := s.db.Query(ctx, `SELECT `+watchColumns+` FROM search_watches WHERE account_id = @account ORDER BY id DESC`,
		pgx.StrictNamedArgs{"account": account})
	if err != nil {
		return nil, fmt.Errorf("select watches: %w", err)
	}
	return collectWatches(rows)
}

func (s pgWatches) ByKey(ctx context.Context, account uuid.UUID, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT id FROM search_watches WHERE account_id = @account AND query_key = @key`,
		pgx.StrictNamedArgs{"account": account, "key": key}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, false, nil
	}
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("select watch: %w", err)
	}
	return id, true, nil
}

func (s pgWatches) SetAlert(ctx context.Context, account, id uuid.UUID, on bool) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE search_watches SET alert = @on WHERE id = @id AND account_id = @account`,
		pgx.StrictNamedArgs{"id": id, "account": account, "on": on})
	if err != nil {
		return false, fmt.Errorf("set watch alert: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s pgWatches) Seen(ctx context.Context, account, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE search_watches SET new_count = 0 WHERE id = @id AND account_id = @account`,
		pgx.StrictNamedArgs{"id": id, "account": account}); err != nil {
		return fmt.Errorf("see watch: %w", err)
	}
	return nil
}

func (s pgWatches) Delete(ctx context.Context, account, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM search_watches WHERE id = @id AND account_id = @account`,
		pgx.StrictNamedArgs{"id": id, "account": account}); err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	return nil
}

func (s pgWatches) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM search_watches WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear watches: %w", err)
	}
	return nil
}

func (s pgWatches) Arrive(ctx context.Context, listing uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO listing_arrivals (listing_id) VALUES (@listing) ON CONFLICT (listing_id) DO NOTHING`,
			pgx.StrictNamedArgs{"listing": listing}); err != nil {
			return fmt.Errorf("insert arrival: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM listing_arrivals WHERE seq IN (
    SELECT seq FROM listing_arrivals WHERE arrived_at < now() - make_interval(secs => @keep) ORDER BY seq LIMIT 1000)`,
			pgx.StrictNamedArgs{"keep": domain.KeepArrivals.Seconds()}); err != nil {
			return fmt.Errorf("delete old arrivals: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("arrive: %w", err)
	}
	return nil
}

func (s pgWatches) Arrivals(ctx context.Context, after int64, lag time.Duration, limit int) ([]Arrival, error) {
	// A prefix: an arrival younger than the lag holds back every higher seq,
	// since arrived_at is its transaction's start and a slow transaction can
	// take a lower seq yet a later time.
	rows, err := s.db.Query(ctx, `SELECT seq, listing_id FROM listing_arrivals
WHERE seq > @after AND seq < coalesce((SELECT min(seq) FROM listing_arrivals
    WHERE seq > @after AND arrived_at > now() - make_interval(secs => @lag)), 9223372036854775807)
ORDER BY seq LIMIT @limit`,
		pgx.StrictNamedArgs{"after": after, "lag": lag.Seconds(), "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select arrivals: %w", err)
	}
	as, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Arrival])
	if err != nil {
		return nil, fmt.Errorf("read arrivals: %w", err)
	}
	return as, nil
}

func (s pgWatches) Swept(ctx context.Context, id uuid.UUID, prev, through int64, n int, at time.Time) (bool, error) {
	moved := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			count int
			alert bool
		)
		err := tx.QueryRow(ctx, `UPDATE search_watches SET last_arrival = @through, new_count = least(@max, new_count + @n)
WHERE id = @id AND last_arrival = @prev RETURNING new_count, alert`,
			pgx.StrictNamedArgs{"id": id, "prev": prev, "through": through, "n": n, "max": domain.MaxNewCount}).Scan(&count, &alert)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("update watch: %w", err)
		}
		moved = true
		if n == 0 || !alert {
			return nil
		}
		return events.Insert(ctx, tx, events.Event{
			ID: uuid.NewV7(), Kind: events.SearchWatchMatched, SubjectID: id, Payload: map[string]any{"count": count}, OccurredAt: at,
		})
	})
	if err != nil {
		return false, fmt.Errorf("sweep watch: %w", err)
	}
	return moved, nil
}

func (s pgWatches) Candidates(ctx context.Context, f CandidateFilter) ([]WatchCandidate, error) {
	rows, err := s.db.Query(ctx, `SELECT w.query, w.phrases, coalesce(w.price_min, 0), coalesce(w.price_max, 0),
    w.offers_only, coalesce(w.radius_km, 0), coalesce(c.lat, 0), coalesce(c.lng, 0)
FROM search_watches AS w
JOIN accounts AS a ON a.id = w.account_id AND a.status = 'active'
LEFT JOIN communes AS c ON c.code = w.commune_code
WHERE (w.category IS NULL OR w.category = @code OR w.category = @root)
  AND (cardinality(w.conditions) = 0 OR @condition = ANY(w.conditions))
  AND w.account_id <> ALL(@exclude)
ORDER BY w.created_at, w.id
LIMIT @limit`, pgx.StrictNamedArgs{
		"code": f.CategoryCode, "root": f.CategoryRoot, "condition": f.Condition, "exclude": f.Exclude, "limit": f.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("watch candidates: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (WatchCandidate, error) {
		var w WatchCandidate
		err := row.Scan(&w.Query, &w.Phrases, &w.PriceMin, &w.PriceMax, &w.OffersOnly, &w.RadiusKm, &w.Point.Lat, &w.Point.Lng)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("read watch candidates: %w", err)
	}
	return out, nil
}
