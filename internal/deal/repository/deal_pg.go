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

const orderColumns = `o.id, o.code, o.status, o.price, o.recipient_name, o.recipient_phone, o.recipient_street,
       o.recipient_commune, o.waybill_source, o.carrier, o.carrier_name, o.tracking_code, o.fee, o.cancelled_by,
       o.cancel_reason, o.requested_at, o.shipped_at, o.closed_at`

// selectDeal reads deals as d with each one's latest order.
const selectDeal = `SELECT d.id, d.listing_id, d.buyer_id, d.seller_id, d.status, d.open_by, d.open_amount, d.open_at,
       d.listed_price, d.agreed_price, d.agreed_at, d.write_id, d.created_at, d.updated_at, ` + orderColumns + `
FROM deals d
LEFT JOIN LATERAL (SELECT id FROM orders WHERE orders.deal_id = d.id ORDER BY orders.id DESC LIMIT 1) latest ON true
LEFT JOIN orders o ON o.id = latest.id
`

// orderScan holds an order's nullable columns while a row is scanned.
type orderScan struct {
	id                                     *uuid.UUID
	code, status                           *string
	price                                  *int64
	name, phone, street, commune           *string
	source, carrier, carrierName, tracking *string
	fee                                    *int64
	cancelledBy, cancelReason              *string
	requestedAt                            *time.Time
	shippedAt, closedAt                    *time.Time
}

func (o *orderScan) dest() []any {
	return []any{
		&o.id, &o.code, &o.status, &o.price, &o.name, &o.phone, &o.street, &o.commune, &o.source, &o.carrier,
		&o.carrierName, &o.tracking, &o.fee, &o.cancelledBy, &o.cancelReason, &o.requestedAt, &o.shippedAt, &o.closedAt,
	}
}

// order is the scanned order, nil when the row had none.
func (o *orderScan) order() *domain.Order {
	if o.id == nil {
		return nil
	}
	out := &domain.Order{
		ID: *o.id, Code: *o.code, Status: domain.OrderStatus(*o.status), Price: *o.price,
		Recipient:   domain.Recipient{Name: *o.name, Phone: *o.phone, Street: *o.street, CommuneCode: *o.commune},
		RequestedAt: *o.requestedAt, ShippedAt: o.shippedAt, ClosedAt: o.closedAt,
	}
	if o.source != nil {
		out.Waybill = &domain.Waybill{
			Source: *o.source, Carrier: domain.Carrier(*o.carrier), TrackingCode: *o.tracking, Fee: *o.fee,
		}
		if o.carrierName != nil {
			out.Waybill.CarrierName = *o.carrierName
		}
	}
	if o.cancelledBy != nil {
		out.CancelledBy, out.CancelReason = domain.Party(*o.cancelledBy), domain.CancelReason(*o.cancelReason)
	}
	return out
}

func scanDeal(row pgx.Row) (domain.Deal, error) {
	var (
		st         domain.DealState
		openBy     *domain.Party
		openAmount *int64
		openAt     *time.Time
		agreed     *int64
	)
	var o orderScan
	dest := append([]any{
		&st.ID, &st.ListingID, &st.BuyerID, &st.SellerID, &st.Status, &openBy, &openAmount, &openAt,
		&st.ListedPrice, &agreed, &st.AgreedAt, &st.WriteID, &st.CreatedAt, &st.UpdatedAt,
	}, o.dest()...)
	if err := row.Scan(dest...); err != nil {
		return domain.Deal{}, fmt.Errorf("scan deal: %w", err)
	}
	st.Order = o.order()
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
	if st.Status == domain.StatusAgreed || st.Status == domain.StatusSold {
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
	return s.one(ctx, selectDeal+`WHERE d.id = @id`, pgx.StrictNamedArgs{"id": id})
}

func (s pgDeals) Find(ctx context.Context, listingID, buyer uuid.UUID) (domain.Deal, error) {
	return s.one(ctx, selectDeal+`WHERE d.listing_id = @listing_id AND d.buyer_id = @buyer_id`,
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
WHERE id = @id AND write_id = @read_write_id`

const insertMove = `
INSERT INTO deal_moves (id, deal_id, kind, by_party, amount, moved_at)
VALUES (@id, @deal_id, @kind, @by_party, @amount, @moved_at)
ON CONFLICT (id) DO NOTHING`

const countOpenOrders = `
SELECT count(*) FROM orders WHERE buyer_id = @buyer AND status IN ('requested', 'shipped') AND id <> @id`

const upsertOrder = `
INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
                    recipient_street, recipient_commune, waybill_source, carrier, carrier_name, tracking_code, fee,
                    cancelled_by, cancel_reason, requested_at, shipped_at, closed_at)
VALUES (@id, @deal_id, @listing_id, @buyer_id, @seller_id, @status, @price, @recipient_name, @recipient_phone,
        @recipient_street, @recipient_commune, @waybill_source, @carrier, @carrier_name, @tracking_code, @fee,
        @cancelled_by, @cancel_reason, @requested_at, @shipped_at, @closed_at)
ON CONFLICT (id) DO UPDATE
SET status = excluded.status, waybill_source = excluded.waybill_source, carrier = excluded.carrier,
    carrier_name = excluded.carrier_name, tracking_code = excluded.tracking_code, fee = excluded.fee,
    cancelled_by = excluded.cancelled_by, cancel_reason = excluded.cancel_reason, shipped_at = excluded.shipped_at,
    closed_at = excluded.closed_at`

// errSaved ends a save whose row already has its write ID: an earlier try
// of the same save committed.
var errSaved = errors.New("deal: saved already")

// orderArgs are the order's columns as named arguments, NULL where it has
// nothing.
func orderArgs(st domain.DealState, o domain.Order) pgx.StrictNamedArgs {
	a := pgx.StrictNamedArgs{
		"id": o.ID, "deal_id": st.ID, "listing_id": st.ListingID, "buyer_id": st.BuyerID, "seller_id": st.SellerID,
		"status": o.Status, "price": o.Price, "recipient_name": o.Recipient.Name,
		"recipient_phone": o.Recipient.Phone, "recipient_street": o.Recipient.Street,
		"recipient_commune": o.Recipient.CommuneCode, "waybill_source": nil, "carrier": nil, "carrier_name": nil,
		"tracking_code": nil, "fee": nil, "cancelled_by": nil, "cancel_reason": nil, "requested_at": o.RequestedAt,
		"shipped_at": o.ShippedAt, "closed_at": o.ClosedAt,
	}
	if w := o.Waybill; w != nil {
		a["waybill_source"], a["carrier"], a["tracking_code"], a["fee"] = w.Source, w.Carrier, w.TrackingCode, w.Fee
		if w.CarrierName != "" {
			a["carrier_name"] = w.CarrierName
		}
	}
	if o.CancelledBy != "" {
		a["cancelled_by"], a["cancel_reason"] = o.CancelledBy, o.CancelReason
	}
	return a
}

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
			var saved bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM deals WHERE id = @id AND write_id = @write_id)`,
				pgx.StrictNamedArgs{"id": st.ID, "write_id": writeID}).Scan(&saved); err != nil {
				return fmt.Errorf("check deal: %w", err)
			}
			if saved {
				return errSaved
			}
			return ErrConflict
		}
		if o := st.Order; o != nil {
			if o.Status == domain.OrderRequested {
				if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@buyer::text))`,
					pgx.StrictNamedArgs{"buyer": st.BuyerID}); err != nil {
					return fmt.Errorf("lock buyer: %w", err)
				}
				var n int
				if err = tx.QueryRow(ctx, countOpenOrders, pgx.StrictNamedArgs{"buyer": st.BuyerID, "id": o.ID}).Scan(&n); err != nil {
					return fmt.Errorf("count open orders: %w", err)
				}
				if n >= domain.MaxOpenOrders {
					return domain.ErrCodLimit
				}
			}
			if _, err = tx.Exec(ctx, upsertOrder, orderArgs(st, *o)); err != nil {
				return fmt.Errorf("write order: %w", err)
			}
		}
		for _, m := range d.Pending() {
			var amount *int64
			if m.Amount != 0 {
				amount = &m.Amount
			}
			if _, err = tx.Exec(ctx, insertMove, pgx.StrictNamedArgs{
				"id": m.ID, "deal_id": st.ID, "kind": m.Kind, "by_party": m.By, "amount": amount, "moved_at": m.At,
			}); err != nil {
				return fmt.Errorf("insert move: %w", err)
			}
		}
		for _, e := range evs {
			if err = events.Insert(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSaved) {
		return fmt.Errorf("save deal: %w", err)
	}
	d.Saved(writeID)
	return nil
}

func (s pgDeals) OfListing(ctx context.Context, listingID uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, selectDeal+`WHERE d.listing_id = @listing_id ORDER BY d.id`, pgx.StrictNamedArgs{"listing_id": listingID})
}

const (
	dealsOfBuyer  = selectDeal + `WHERE d.buyer_id = @account AND d.id < @before ORDER BY d.id DESC LIMIT @limit`
	dealsOfSeller = selectDeal + `WHERE d.seller_id = @account AND d.id < @before ORDER BY d.id DESC LIMIT @limit`
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

const (
	ordersOfBuyer = `SELECT o.deal_id, o.listing_id, o.buyer_id, o.seller_id, ` + orderColumns + `
FROM orders o WHERE o.buyer_id = @account AND o.id < @before ORDER BY o.id DESC LIMIT @limit`
	ordersOfSeller = `SELECT o.deal_id, o.listing_id, o.buyer_id, o.seller_id, ` + orderColumns + `
FROM orders o WHERE o.seller_id = @account AND o.id < @before ORDER BY o.id DESC LIMIT @limit`
)

func (s pgDeals) ListOrders(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]OrderRow, error) {
	q := ordersOfBuyer
	if as == domain.Seller {
		q = ordersOfSeller
	}
	rows, err := s.db.Query(ctx, q, pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select orders: %w", err)
	}
	defer rows.Close()
	var out []OrderRow
	for rows.Next() {
		var (
			r OrderRow
			o orderScan
		)
		if err = rows.Scan(append([]any{&r.DealID, &r.ListingID, &r.BuyerID, &r.SellerID}, o.dest()...)...); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		r.Order = *o.order()
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read orders: %w", err)
	}
	return out, nil
}
