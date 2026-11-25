package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgImports struct{ db *pgxpool.Pool }

func NewImports(db *pgxpool.Pool) Imports { return pgImports{db} }

// importColumns are an Import's columns in its field order.
const importColumns = `id, account_id, list_id, oid, import_state, listing_id, review, missing_spec_keys,
coalesce(reason, ''), draft, ad_price, ad_gone, updated_at`

func scanImport(row pgx.Row) (domain.Import, error) {
	var i domain.Import
	err := row.Scan(&i.ID, &i.AccountID, &i.ListID, &i.OID, &i.State, &i.ListingID, &i.Review, &i.MissingSpecKeys,
		&i.Reason, &i.Draft, &i.AdPrice, &i.AdGone, &i.UpdatedAt)
	return i, err //nolint:wrapcheck // the caller wraps
}

func collectImports(rows pgx.Rows, err error) ([]domain.Import, error) {
	if err != nil {
		return nil, fmt.Errorf("read imports: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Import, error) { return scanImport(row) })
	if err != nil {
		return nil, fmt.Errorf("read imports: %w", err)
	}
	return out, nil
}

// importArgs are imp's columns as named arguments.
func importArgs(imp domain.Import) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id": imp.ID, "account": imp.AccountID, "list_id": imp.ListID, "oid": imp.OID, "state": imp.State,
		"listing": imp.ListingID, "review": imp.Review, "missing": imp.MissingSpecKeys, "reason": imp.Reason,
		"draft": imp.Draft, "ad_price": imp.AdPrice, "ad_gone": imp.AdGone, "updated_at": imp.UpdatedAt,
	}
}

func (s pgImports) Start(ctx context.Context, imps []domain.Import, now time.Time) ([]domain.Import, error) {
	if len(imps) == 0 {
		return []domain.Import{}, nil
	}
	account := imps[0].AccountID
	ids := make([]int64, len(imps))
	for i := range imps {
		ids[i] = imps[i].ListID
	}
	out := make([]domain.Import, len(imps))
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
		// Two starts of one account wait for each other, so a listing
		// starts once.
		postgres.QueueLock(b, "chotot_imports", account)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		have, err := collectImports(tx.Query(ctx, `SELECT `+importColumns+` FROM chotot_imports
WHERE account_id = @account AND list_id = ANY(@ids)`, pgx.StrictNamedArgs{"account": account, "ids": ids}))
		if err != nil {
			return err
		}
		byList := make(map[int64]*domain.Import, len(have))
		for i := range have {
			byList[have[i].ListID] = &have[i]
		}
		for i := range imps {
			imp := imps[i]
			if h, ok := byList[imp.ListID]; ok && h.Settled(now) {
				out[i] = *h
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO chotot_imports (id, account_id, list_id, oid, import_state, listing_id,
    review, missing_spec_keys, reason, draft, ad_price, ad_gone, updated_at)
VALUES (@id, @account, @list_id, @oid, @state, @listing, @review, @missing, NULLIF(@reason, ''), @draft, @ad_price,
    @ad_gone, @updated_at)
ON CONFLICT (account_id, list_id) DO UPDATE SET id = excluded.id, oid = excluded.oid,
    import_state = excluded.import_state, listing_id = excluded.listing_id, review = excluded.review,
    missing_spec_keys = excluded.missing_spec_keys, reason = excluded.reason, draft = excluded.draft,
    ad_price = excluded.ad_price, ad_gone = excluded.ad_gone, updated_at = excluded.updated_at`,
				importArgs(imp)); err != nil {
				return err //nolint:wrapcheck // wrapped below
			}
			out[i] = imp
		}
		return nil
	})
	if errors.Is(err, accountdomain.ErrSessionRevoked) {
		return nil, err //nolint:wrapcheck // a refusal
	}
	if err != nil {
		return nil, fmt.Errorf("start imports: %w", err)
	}
	return out, nil
}

func (s pgImports) Save(ctx context.Context, imp domain.Import, from domain.ImportState) (bool, error) {
	args := importArgs(imp)
	args["from"] = from
	tag, err := s.db.Exec(ctx, `UPDATE chotot_imports SET import_state = @state, listing_id = @listing, review = @review,
    missing_spec_keys = @missing, reason = NULLIF(@reason, ''), draft = @draft, ad_price = @ad_price,
    ad_gone = @ad_gone, updated_at = @updated_at
WHERE id = @id AND import_state = @from
    AND account_id = @account AND list_id = @list_id AND oid = @oid`, args)
	if err != nil {
		return false, fmt.Errorf("save import: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgImports) one(ctx context.Context, where string, args pgx.StrictNamedArgs) (domain.Import, bool, error) {
	i, err := scanImport(s.db.QueryRow(ctx, `SELECT `+importColumns+` FROM chotot_imports WHERE `+where, args))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Import{}, false, nil
	}
	if err != nil {
		return domain.Import{}, false, fmt.Errorf("read import: %w", err)
	}
	return i, true, nil
}

func (s pgImports) Get(ctx context.Context, account uuid.UUID, listID int64) (domain.Import, bool, error) {
	return s.one(ctx, `account_id = @account AND list_id = @list_id`, pgx.StrictNamedArgs{"account": account, "list_id": listID})
}

func (s pgImports) ByID(ctx context.Context, id uuid.UUID) (domain.Import, bool, error) {
	return s.one(ctx, `id = @id`, pgx.StrictNamedArgs{"id": id})
}

func (s pgImports) ByListing(ctx context.Context, listing uuid.UUID) (domain.Import, bool, error) {
	return s.one(ctx, `listing_id = @listing`, pgx.StrictNamedArgs{"listing": listing})
}

func (s pgImports) List(ctx context.Context, account uuid.UUID) ([]domain.Import, error) {
	return collectImports(s.db.Query(ctx, `SELECT `+importColumns+` FROM chotot_imports WHERE account_id = @account
ORDER BY list_id DESC`, pgx.StrictNamedArgs{"account": account}))
}

func (s pgImports) Listed(ctx context.Context, account uuid.UUID, listings []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(listings))
	if len(listings) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `SELECT listing_id FROM chotot_imports
WHERE account_id = @account AND listing_id = ANY(@listings)`, pgx.StrictNamedArgs{"account": account, "listings": listings})
	if err != nil {
		return nil, fmt.Errorf("read imported listings: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read imported listings: %w", err)
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func (s pgImports) Followed(ctx context.Context, id uuid.UUID, adPrice int64, adGone bool) error {
	if _, err := s.db.Exec(ctx, `UPDATE chotot_imports SET ad_price = @price, ad_gone = @gone WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id, "price": adPrice, "gone": adGone}); err != nil {
		return fmt.Errorf("record the listing on Chợ Tốt: %w", err)
	}
	return nil
}

func (s pgImports) ClearReview(ctx context.Context, listing uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE chotot_imports SET review = '{}' WHERE listing_id = @listing AND review <> '{}'`,
		pgx.StrictNamedArgs{"listing": listing}); err != nil {
		return fmt.Errorf("clear review: %w", err)
	}
	return nil
}

func (s pgImports) DeleteListing(ctx context.Context, listing uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM chotot_imports WHERE listing_id = @listing`,
		pgx.StrictNamedArgs{"listing": listing}); err != nil {
		return fmt.Errorf("delete import: %w", err)
	}
	return nil
}

func (s pgImports) Sold(ctx context.Context, id, event uuid.UUID, at time.Time) error {
	b := &pgx.Batch{}
	events.Queue(b, events.Event{ID: event, Kind: events.ChototImportSold, SubjectID: id, Payload: map[string]any{}, OccurredAt: at})
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return tx.SendBatch(ctx, b).Close() }); err != nil {
		return fmt.Errorf("announce sale: %w", err)
	}
	return nil
}
