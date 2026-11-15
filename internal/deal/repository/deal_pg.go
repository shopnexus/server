package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/stream"
)

type pgDeals struct{ db *pgxpool.Pool }

func NewDeals(db *pgxpool.Pool) Deals { return pgDeals{db} }

const dealColumns = `id, conversation_id, listing_id, buyer_id, seller_id, status, open_by, open_amount, open_at, listed_price,
       agreed_price, agreed_at, write_id, created_at, updated_at`

const orderColumns = `o.id, o.code, o.status, o.price, o.recipient_name, o.recipient_phone, o.recipient_street,
       o.recipient_commune, o.recipient_kind, o.shipment_mode, o.carrier, o.tracking_code, o.fee, o.shipment_status, o.delivered_at,
       o.pickup_point_id, o.pickup_name, o.pickup_phone, o.pickup_street, o.pickup_commune,
       o.cancelled_by, o.cancel_reason, o.completed_by, o.requested_at, o.shipped_at, o.closed_at`

// selectDeal reads deals as d with each one's latest order.
const selectDeal = `SELECT d.id, d.conversation_id, d.listing_id, d.buyer_id, d.seller_id, d.status, d.open_by, d.open_amount, d.open_at,
       d.listed_price, d.agreed_price, d.agreed_at, d.write_id, d.created_at, d.updated_at, ` + orderColumns + `
FROM deals d
LEFT JOIN LATERAL (SELECT id FROM orders WHERE orders.deal_id = d.id ORDER BY orders.id DESC LIMIT 1) latest ON true
LEFT JOIN orders o ON o.id = latest.id
`

// orderScan holds an order's nullable columns while a row is scanned.
type orderScan struct {
	id                             *uuid.UUID
	code, status                   *string
	price                          *int64
	name, phone, street, commune   *string
	kind                           *string
	mode, carrier, tracking, state *string
	fee                            *int64
	deliveredAt                    *time.Time
	pickupPoint, pickupName        *string
	pickupPhone, pickupStreet      *string
	pickupCommune                  *string
	cancelledBy, cancelReason      *string
	completedBy                    *string
	requestedAt                    *time.Time
	shippedAt, closedAt            *time.Time
}

func (o *orderScan) dest() []any {
	return []any{
		&o.id, &o.code, &o.status, &o.price, &o.name, &o.phone, &o.street, &o.commune, &o.kind, &o.mode, &o.carrier, &o.tracking,
		&o.fee, &o.state, &o.deliveredAt, &o.pickupPoint, &o.pickupName, &o.pickupPhone, &o.pickupStreet, &o.pickupCommune,
		&o.cancelledBy, &o.cancelReason, &o.completedBy, &o.requestedAt, &o.shippedAt, &o.closedAt,
	}
}

// order is the scanned order, nil when the row had none.
func (o *orderScan) order() *domain.Order {
	if o.id == nil {
		return nil
	}
	out := &domain.Order{
		ID: *o.id, Code: *o.code, Status: domain.OrderStatus(*o.status), Price: *o.price,
		Recipient: domain.Recipient{
			Name: *o.name, Phone: *o.phone, Street: *o.street, CommuneCode: *o.commune, Kind: domain.AddressKind(*o.kind),
		},
		RequestedAt: *o.requestedAt, ShippedAt: o.shippedAt, ClosedAt: o.closedAt,
	}
	if o.mode != nil {
		out.Shipment = &domain.Shipment{
			Mode: domain.ShipmentMode(*o.mode), Carrier: domain.Carrier(*o.carrier), TrackingCode: *o.tracking,
			Fee: o.fee, Status: domain.ShipmentStatus(*o.state), DeliveredAt: o.deliveredAt,
		}
		if o.pickupPoint != nil || o.pickupName != nil || o.pickupCommune != nil {
			out.Shipment.Pickup = &domain.ShipmentPickup{
				PointID: deref(o.pickupPoint), Name: deref(o.pickupName), Phone: deref(o.pickupPhone),
				Street: deref(o.pickupStreet), CommuneCode: deref(o.pickupCommune),
			}
		}
	}
	if o.completedBy != nil {
		out.CompletedBy = domain.Party(*o.completedBy)
	}
	if o.cancelledBy != nil {
		out.CancelledBy, out.CancelReason = domain.Party(*o.cancelledBy), domain.CancelReason(*o.cancelReason)
	}
	return out
}

// deref is *p, or "" for nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// orNull is s, or NULL for "".
func orNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanDeal(row pgx.CollectableRow) (domain.Deal, error) {
	var (
		st         domain.DealState
		openBy     *domain.Party
		openAmount *int64
		openAt     *time.Time
		agreed     *int64
	)
	var o orderScan
	dest := append([]any{
		&st.ID, &st.ConversationID, &st.ListingID, &st.BuyerID, &st.SellerID, &st.Status, &openBy, &openAmount, &openAt,
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
		"id": st.ID, "conversation_id": st.ConversationID, "listing_id": st.ListingID, "buyer_id": st.BuyerID, "seller_id": st.SellerID,
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
VALUES (@id, @conversation_id, @listing_id, @buyer_id, @seller_id, @status, @open_by, @open_amount, @open_at, @listed_price,
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
	rows, err := s.db.Query(ctx, q, a)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("select deal: %w", err)
	}
	d, err := pgx.CollectOneRow(rows, scanDeal)
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
SELECT count(*) FROM orders WHERE buyer_id = @buyer AND status = ANY(@open) AND id <> @id`

const upsertOrder = `
INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
                    recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, fee, shipment_status,
                    delivered_at, pickup_point_id, pickup_name, pickup_phone, pickup_street, pickup_commune,
                    cancelled_by, cancel_reason, completed_by, requested_at, shipped_at, closed_at)
VALUES (@id, @deal_id, @listing_id, @buyer_id, @seller_id, @status, @price, @recipient_name, @recipient_phone,
        @recipient_street, @recipient_commune, @recipient_kind, @shipment_mode, @carrier, @tracking_code, @fee, @shipment_status,
        @delivered_at, @pickup_point_id, @pickup_name, @pickup_phone, @pickup_street, @pickup_commune,
        @cancelled_by, @cancel_reason, @completed_by, @requested_at, @shipped_at, @closed_at)
ON CONFLICT (id) DO UPDATE
SET status = excluded.status, shipment_mode = excluded.shipment_mode, carrier = excluded.carrier,
    tracking_code = excluded.tracking_code, fee = excluded.fee, shipment_status = excluded.shipment_status,
    delivered_at = excluded.delivered_at, pickup_point_id = excluded.pickup_point_id, pickup_name = excluded.pickup_name,
    pickup_phone = excluded.pickup_phone, pickup_street = excluded.pickup_street, pickup_commune = excluded.pickup_commune,
    cancelled_by = excluded.cancelled_by, cancel_reason = excluded.cancel_reason, completed_by = excluded.completed_by,
    shipped_at = excluded.shipped_at, closed_at = excluded.closed_at`

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
		"recipient_commune": o.Recipient.CommuneCode, "recipient_kind": o.Recipient.Kind, "shipment_mode": nil, "carrier": nil, "tracking_code": nil,
		"fee": nil, "shipment_status": nil, "delivered_at": nil, "pickup_point_id": nil, "pickup_name": nil,
		"pickup_phone": nil, "pickup_street": nil, "pickup_commune": nil, "cancelled_by": nil, "cancel_reason": nil,
		"completed_by": nil, "requested_at": o.RequestedAt, "shipped_at": o.ShippedAt, "closed_at": o.ClosedAt,
	}
	if s := o.Shipment; s != nil {
		a["shipment_mode"], a["carrier"], a["tracking_code"] = s.Mode, s.Carrier, s.TrackingCode
		a["fee"], a["shipment_status"], a["delivered_at"] = s.Fee, s.Status, s.DeliveredAt
		if p := s.Pickup; p != nil {
			a["pickup_point_id"], a["pickup_name"], a["pickup_phone"] = orNull(p.PointID), orNull(p.Name), orNull(p.Phone)
			a["pickup_street"], a["pickup_commune"] = orNull(p.Street), orNull(p.CommuneCode)
		}
	}
	if o.CancelledBy != "" {
		a["cancelled_by"], a["cancel_reason"] = o.CancelledBy, o.CancelReason
	}
	if o.CompletedBy != "" {
		a["completed_by"] = o.CompletedBy
	}
	return a
}

func (s pgDeals) Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error {
	st := d.State()
	a := dealArgs(st)
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	for _, k := range []string{"conversation_id", "listing_id", "buyer_id", "seller_id", "created_at"} {
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
			if o.Status == domain.OrderPending {
				if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@buyer::text))`,
					pgx.StrictNamedArgs{"buyer": st.BuyerID}); err != nil {
					return fmt.Errorf("lock buyer: %w", err)
				}
				var n int
				if err = tx.QueryRow(ctx, countOpenOrders, pgx.StrictNamedArgs{"buyer": st.BuyerID, "id": o.ID, "open": domain.OpenStatuses}).Scan(&n); err != nil {
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
		// The moves' IDs ride on deal.moved, so a subscriber reads exactly
		// the moves of this save. The copy leaves the caller's events alone.
		out := slices.Clone(evs)
		for i := range out {
			if out[i].Kind == events.DealMoved {
				ids := make([]string, 0, len(d.Pending()))
				for _, m := range d.Pending() {
					ids = append(ids, m.ID.String())
				}
				out[i].Payload = map[string]any{"moves": ids}
			}
		}
		if err = events.Insert(ctx, tx, out...); err != nil {
			return err
		}
		return stream.Notify(ctx, tx, []uuid.UUID{st.BuyerID, st.SellerID}, stream.Hint{Kind: stream.KindDeal, DealID: &st.ID})
	})
	if err != nil && !errors.Is(err, errSaved) {
		return fmt.Errorf("save deal: %w", err)
	}
	d.Saved(writeID)
	return nil
}

func (s pgDeals) Between(ctx context.Context, a, b uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, selectDeal+`WHERE (d.buyer_id = @a AND d.seller_id = @b) OR (d.buyer_id = @b AND d.seller_id = @a)
ORDER BY d.id DESC`, pgx.StrictNamedArgs{"a": a, "b": b})
}

// dealsOfPairs ranks each pair's deals by last move, a UUIDv7 as the deal
// ID is, and keeps the active ones and the newest.
const dealsOfPairs = selectDeal + `JOIN (
    SELECT id, status, last_move, row_number() OVER (PARTITION BY other ORDER BY last_move DESC) AS nth
    FROM (
        SELECT d.id, d.status, CASE WHEN d.buyer_id = @viewer THEN d.seller_id ELSE d.buyer_id END AS other,
               coalesce((SELECT m.id FROM deal_moves m WHERE m.deal_id = d.id ORDER BY m.id DESC LIMIT 1), d.id)
                   AS last_move
        FROM deals d
        WHERE (d.buyer_id = @viewer AND d.seller_id = ANY(@others))
           OR (d.seller_id = @viewer AND d.buyer_id = ANY(@others))
    ) pair
) ranked ON ranked.id = d.id AND (ranked.status IN ('negotiating', 'agreed') OR ranked.nth = 1)
ORDER BY ranked.last_move DESC`

func (s pgDeals) OfPairs(ctx context.Context, viewer uuid.UUID, others []uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, dealsOfPairs, pgx.StrictNamedArgs{"viewer": viewer, "others": others})
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
	out, err := pgx.CollectRows(rows, scanDeal)
	if err != nil {
		return nil, fmt.Errorf("read deals: %w", err)
	}
	return out, nil
}

// dealMove is a move with its deal, as Moves scans it.
type dealMove struct {
	domain.Move

	DealID uuid.UUID
}

func (s pgDeals) Moves(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.Move, error) {
	rows, err := s.db.Query(ctx, `SELECT id, kind, by_party, coalesce(amount, 0), moved_at, deal_id FROM deal_moves
WHERE deal_id = ANY(@ids) ORDER BY id`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select moves: %w", err)
	}
	moves, err := pgx.CollectRows(rows, pgx.RowToStructByPos[dealMove])
	if err != nil {
		return nil, fmt.Errorf("read moves: %w", err)
	}
	out := map[uuid.UUID][]domain.Move{}
	for _, m := range moves {
		out[m.DealID] = append(out[m.DealID], m.Move)
	}
	return out, nil
}

func (s pgDeals) MovesOf(ctx context.Context, deal uuid.UUID, ids []uuid.UUID) ([]domain.Move, error) {
	rows, err := s.db.Query(ctx, `SELECT id, kind, by_party, coalesce(amount, 0), moved_at FROM deal_moves
WHERE deal_id = @deal AND id = ANY(@ids) ORDER BY id`, pgx.StrictNamedArgs{"deal": deal, "ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select moves: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Move])
	if err != nil {
		return nil, fmt.Errorf("read moves: %w", err)
	}
	return out, nil
}

// selectOrder reads orders as o with their deals.
const selectOrder = `SELECT o.deal_id, o.listing_id, o.buyer_id, o.seller_id, ` + orderColumns + `
FROM orders o
`

const (
	ordersOfBuyer  = selectOrder + `WHERE o.buyer_id = @account AND o.id < @before ORDER BY o.id DESC LIMIT @limit`
	ordersOfSeller = selectOrder + `WHERE o.seller_id = @account AND o.id < @before ORDER BY o.id DESC LIMIT @limit`
)

func scanOrder(row pgx.CollectableRow) (OrderRow, error) {
	var (
		r OrderRow
		o orderScan
	)
	if err := row.Scan(append([]any{&r.DealID, &r.ListingID, &r.BuyerID, &r.SellerID}, o.dest()...)...); err != nil {
		return OrderRow{}, fmt.Errorf("scan order: %w", err)
	}
	r.Order = *o.order()
	return r, nil
}

func (s pgDeals) ListOrders(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]OrderRow, error) {
	q := ordersOfBuyer
	if as == domain.Seller {
		q = ordersOfSeller
	}
	rows, err := s.db.Query(ctx, q, pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select orders: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, fmt.Errorf("read orders: %w", err)
	}
	return out, nil
}

func (s pgDeals) Order(ctx context.Context, id uuid.UUID) (OrderRow, error) {
	rows, err := s.db.Query(ctx, selectOrder+`WHERE o.id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return OrderRow{}, fmt.Errorf("select order: %w", err)
	}
	r, err := pgx.CollectOneRow(rows, scanOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderRow{}, ErrNotFound
	}
	if err != nil {
		return OrderRow{}, fmt.Errorf("read order: %w", err)
	}
	return r, nil
}

func (s pgDeals) OrdersWithCode(ctx context.Context, code string) ([]OrderRow, error) {
	rows, err := s.db.Query(ctx, selectOrder+`WHERE o.tracking_code = @code ORDER BY o.id`, pgx.StrictNamedArgs{"code": code})
	if err != nil {
		return nil, fmt.Errorf("select orders: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, fmt.Errorf("read orders: %w", err)
	}
	return out, nil
}
