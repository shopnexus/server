package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/provider/carrier"
)

type pgEvents struct{ db *pgxpool.Pool }

func NewEvents(db *pgxpool.Pool) Events { return pgEvents{db} }

const eventColumns = `at, kind, step, carrier_status, reason, reason_code, warehouse, shipper_name, shipper_phone, proof_url, amount`

func (s pgEvents) Add(ctx context.Context, c, code string, ev carrier.Event, received time.Time) (bool, error) {
	var step *string
	if ev.Step != "" {
		step = new(string(ev.Step))
	}
	tag, err := s.db.Exec(ctx, `INSERT INTO shipment_events (carrier, tracking_code, `+eventColumns+`, received_at)
SELECT @carrier, @code, @at, @kind, @step, @status, @reason, @reason_code, @warehouse, @shipper_name, @shipper_phone,
       @proof_url, @amount, @received
WHERE NOT EXISTS (
    SELECT 1 FROM shipment_events AS e
    WHERE e.carrier = @carrier AND e.tracking_code = @code AND e.kind = @kind AND e.step IS NOT DISTINCT FROM @step
      AND e.carrier_status = @status AND e.amount = @amount
      AND (
          e.at BETWEEN @at::timestamptz - INTERVAL '10 minutes' AND @at::timestamptz + INTERVAL '10 minutes'
          OR e.id = (
              SELECT n.id FROM shipment_events AS n
              WHERE n.carrier = @carrier AND n.tracking_code = @code AND n.kind = @kind
              ORDER BY n.at DESC, n.id DESC LIMIT 1
          )
      )
)`, pgx.StrictNamedArgs{
		"carrier": c, "code": code, "at": ev.At, "kind": string(ev.Kind), "step": step, "status": ev.CarrierStatus,
		"reason": ev.Reason, "reason_code": ev.ReasonCode, "warehouse": ev.Warehouse, "shipper_name": ev.ShipperName,
		"shipper_phone": ev.ShipperPhone, "proof_url": ev.ProofURL, "amount": ev.Amount, "received": received,
	})
	if err != nil {
		return false, fmt.Errorf("insert shipment event: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgEvents) List(ctx context.Context, c, code string) ([]carrier.Event, error) {
	rows, err := s.db.Query(ctx, `SELECT `+eventColumns+` FROM shipment_events
WHERE carrier = @carrier AND tracking_code = @code ORDER BY at, id`, pgx.StrictNamedArgs{"carrier": c, "code": code})
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
