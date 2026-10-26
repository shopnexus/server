package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgDeals struct{ db *pgxpool.Pool }

func NewDeals(db *pgxpool.Pool) Deals { return pgDeals{db} }

const dealColumns = `id, listing_id, buyer_id, seller_id, status, open_by, open_amount, open_at, listed_price,
       agreed_price, agreed_at, write_id, created_at, updated_at`

const selectDeal = `SELECT ` + dealColumns + ` FROM deals `

func scanDeal(row pgx.Row) (domain.Deal, error) {
	var (
		st         domain.DealState
		openBy     *domain.Party
		openAmount *int64
		openAt     *time.Time
		agreed     *int64
	)
	if err := row.Scan(&st.ID, &st.ListingID, &st.BuyerID, &st.SellerID, &st.Status, &openBy, &openAmount, &openAt,
		&st.ListedPrice, &agreed, &st.AgreedAt, &st.WriteID, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return domain.Deal{}, fmt.Errorf("scan deal: %w", err)
	}
	if openBy != nil {
		st.Open = &domain.OpenPrice{By: *openBy, Amount: *openAmount, At: *openAt}
	}
	if agreed != nil {
		st.AgreedPrice = *agreed
	}
	return domain.RestoreDeal(st), nil
}

// dealArgs are a deal's columns as named arguments, NULL where the state
// has nothing.
func dealArgs(st domain.DealState) pgx.StrictNamedArgs {
	a := pgx.StrictNamedArgs{
		"id": st.ID, "listing_id": st.ListingID, "buyer_id": st.BuyerID, "seller_id": st.SellerID,
		"status": st.Status, "open_by": nil, "open_amount": nil, "open_at": nil, "listed_price": st.ListedPrice,
		"agreed_price": nil, "agreed_at": st.AgreedAt, "write_id": st.WriteID, "created_at": st.CreatedAt,
		"updated_at": st.UpdatedAt,
	}
	if o := st.Open; o != nil {
		a["open_by"], a["open_amount"], a["open_at"] = o.By, o.Amount, o.At
	}
	if st.Status == domain.StatusAgreed {
		a["agreed_price"] = st.AgreedPrice
	}
	return a
}

const insertDeal = `
INSERT INTO deals (` + dealColumns + `)
VALUES (@id, @listing_id, @buyer_id, @seller_id, @status, @open_by, @open_amount, @open_at, @listed_price,
        @agreed_price, @agreed_at, @write_id, @created_at, @updated_at)
ON CONFLICT (listing_id, buyer_id) DO NOTHING`

func (s pgDeals) Insert(ctx context.Context, d domain.Deal) (bool, error) {
	tag, err := s.db.Exec(ctx, insertDeal, dealArgs(d.State()))
	if err != nil {
		return false, fmt.Errorf("insert deal: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgDeals) Get(ctx context.Context, id uuid.UUID) (domain.Deal, error) {
	return s.one(ctx, selectDeal+`WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
}

func (s pgDeals) Find(ctx context.Context, listingID, buyer uuid.UUID) (domain.Deal, error) {
	return s.one(ctx, selectDeal+`WHERE listing_id = @listing_id AND buyer_id = @buyer_id`,
		pgx.StrictNamedArgs{"listing_id": listingID, "buyer_id": buyer})
}

// one reads the deal query q finds, or ErrNotFound.
func (s pgDeals) one(ctx context.Context, q string, a pgx.StrictNamedArgs) (domain.Deal, error) {
	d, err := scanDeal(s.db.QueryRow(ctx, q, a))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ErrNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("select deal: %w", err)
	}
	return d, nil
}

const saveDeal = `
UPDATE deals
SET status = @status, open_by = @open_by, open_amount = @open_amount, open_at = @open_at,
    listed_price = @listed_price, agreed_price = @agreed_price, agreed_at = @agreed_at,
    updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

const insertMove = `
INSERT INTO deal_moves (id, deal_id, kind, by_party, amount, moved_at)
VALUES (@id, @deal_id, @kind, @by_party, @amount, @moved_at)
ON CONFLICT (id) DO NOTHING`

func (s pgDeals) Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error {
	st := d.State()
	a := dealArgs(st)
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	for _, k := range []string{"listing_id", "buyer_id", "seller_id", "created_at"} {
		delete(a, k)
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, saveDeal, a)
		if err != nil {
			return fmt.Errorf("update deal: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		for _, m := range d.Pending() {
			var amount *int64
			if m.Amount != 0 {
				amount = &m.Amount
			}
			if _, err := tx.Exec(ctx, insertMove, pgx.StrictNamedArgs{
				"id": m.ID, "deal_id": st.ID, "kind": m.Kind, "by_party": m.By, "amount": amount, "moved_at": m.At,
			}); err != nil {
				return fmt.Errorf("insert move: %w", err)
			}
		}
		for _, e := range evs {
			if err := events.Insert(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save deal: %w", err)
	}
	d.Saved(writeID)
	return nil
}

func (s pgDeals) OfListing(ctx context.Context, listingID uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, selectDeal+`WHERE listing_id = @listing_id ORDER BY id`, pgx.StrictNamedArgs{"listing_id": listingID})
}

const (
	dealsOfBuyer  = selectDeal + `WHERE buyer_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`
	dealsOfSeller = selectDeal + `WHERE seller_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`
)

func (s pgDeals) ListByParty(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]domain.Deal, error) {
	q := dealsOfBuyer
	if as == domain.Seller {
		q = dealsOfSeller
	}
	return s.many(ctx, q, pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
}

// many reads every deal query q finds.
func (s pgDeals) many(ctx context.Context, q string, a pgx.StrictNamedArgs) ([]domain.Deal, error) {
	rows, err := s.db.Query(ctx, q, a)
	if err != nil {
		return nil, fmt.Errorf("select deals: %w", err)
	}
	defer rows.Close()
	var out []domain.Deal
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read deals: %w", err)
	}
	return out, nil
}

func (s pgDeals) Moves(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.Move, error) {
	rows, err := s.db.Query(ctx, `SELECT deal_id, id, kind, by_party, amount, moved_at FROM deal_moves
WHERE deal_id = ANY(@ids) ORDER BY id`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select moves: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]domain.Move{}
	for rows.Next() {
		var (
			deal   uuid.UUID
			m      domain.Move
			amount *int64
		)
		if err := rows.Scan(&deal, &m.ID, &m.Kind, &m.By, &amount, &m.At); err != nil {
			return nil, fmt.Errorf("scan move: %w", err)
		}
		if amount != nil {
			m.Amount = *amount
		}
		out[deal] = append(out[deal], m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read moves: %w", err)
	}
	return out, nil
}
