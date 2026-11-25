package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/provider/carrier"
)

type pgEvents struct{ db *pgxpool.Pool }

func NewEvents(db *pgxpool.Pool) Events { return pgEvents{db} }

const eventColumns = `at, kind, step, carrier_status, reason, reason_code, warehouse, shipper_name, shipper_phone, proof_url, amount`

// sameEventWindow is how far apart a webhook and a poll may date one
// event: they read different fields, and a parse failure dates it now.
const sameEventWindow = 10 * time.Minute

func (s pgEvents) Add(ctx context.Context, c, code string, ev carrier.Event) (bool, error) {
	var step *string
	if ev.Step != "" {
		step = new(string(ev.Step))
	}
	// The parcel's lock makes a webhook and a poll of one event take turns,
	// so the second finds the first; the batch is one transaction.
	b := &pgx.Batch{}
	b.Queue(`SELECT pg_advisory_xact_lock(hashtext('shipment_events:' || @carrier || ':' || @code))`,
		pgx.StrictNamedArgs{"carrier": c, "code": code})
	var added bool
	b.Queue(`INSERT INTO shipment_events (carrier, tracking_code, `+eventColumns+`, received_at)
SELECT @carrier, @code, @at, @kind, @step, @status, @reason, @reason_code, @warehouse, @shipper_name, @shipper_phone,
       @proof_url, @amount, now()
WHERE NOT EXISTS (
    SELECT 1 FROM shipment_events AS e
    WHERE e.carrier = @carrier AND e.tracking_code = @code AND e.kind = @kind AND e.step IS NOT DISTINCT FROM @step
      AND e.carrier_status = @status AND e.amount = @amount
      AND (
          e.at BETWEEN @from AND @to
          OR e.id = (
              SELECT n.id FROM shipment_events AS n
              WHERE n.carrier = @carrier AND n.tracking_code = @code AND n.kind = @kind
              ORDER BY n.at DESC, n.id DESC LIMIT 1
          )
      )
)`, pgx.StrictNamedArgs{
		"carrier": c, "code": code, "at": ev.At, "kind": string(ev.Kind), "step": step, "status": ev.CarrierStatus,
		"reason": ev.Reason, "reason_code": ev.ReasonCode, "warehouse": ev.Warehouse, "shipper_name": ev.ShipperName,
		"shipper_phone": ev.ShipperPhone, "proof_url": ev.ProofURL, "amount": ev.Amount,
		"from": ev.At.Add(-sameEventWindow), "to": ev.At.Add(sameEventWindow),
	}).Exec(func(tag pgconn.CommandTag) error {
		added = tag.RowsAffected() == 1
		return nil
	})
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return false, fmt.Errorf("insert shipment event: %w", err)
	}
	return added, nil
}

func (s pgEvents) List(ctx context.Context, c, code string, stepsOnly bool) ([]carrier.Event, error) {
	rows, err := s.db.Query(ctx, `SELECT `+eventColumns+` FROM shipment_events
WHERE carrier = @carrier AND tracking_code = @code AND (kind = 'step' OR NOT @steps)
ORDER BY at DESC, id DESC`, pgx.StrictNamedArgs{"carrier": c, "code": code, "steps": stepsOnly})
	if err != nil {
		return nil, fmt.Errorf("select shipment events: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (carrier.Event, error) {
		var ev carrier.Event
		var step *string
		if scanErr := r.Scan(&ev.At, &ev.Kind, &step, &ev.CarrierStatus, &ev.Reason, &ev.ReasonCode, &ev.Warehouse,
			&ev.ShipperName, &ev.ShipperPhone, &ev.ProofURL, &ev.Amount); scanErr != nil {
			return carrier.Event{}, fmt.Errorf("scan shipment event: %w", scanErr)
		}
		if step != nil {
			ev.Step = carrier.Step(*step)
		}
		return ev, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read shipment events: %w", err)
	}
	return out, nil
}
