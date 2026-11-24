-- What the carriers reported of a parcel, for its timeline: a step with the
-- carrier's own status, or a new weight, fee or COD amount.
CREATE TABLE shipment_events (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    carrier        text NOT NULL,
    tracking_code  text NOT NULL,
    at             timestamptz NOT NULL,
    kind           text NOT NULL CHECK (kind IN ('step', 'weight', 'fee', 'cod', 'cod_transfer')),
    step           text CHECK (
        step IN (
            'ready_to_pick', 'picking', 'pickup_failed', 'picked', 'at_hub', 'transporting', 'out_for_delivery',
            'delivery_failed', 'delivered', 'returning', 'returned', 'cancelled', 'lost'
        )
    ),
    carrier_status text NOT NULL,
    reason         text NOT NULL,
    reason_code    text NOT NULL,
    warehouse      text NOT NULL,
    shipper_name   text NOT NULL,
    shipper_phone  text NOT NULL,
    proof_url      text NOT NULL,
    amount         bigint NOT NULL CHECK (amount >= 0),
    received_at    timestamptz NOT NULL,
    CHECK ((kind = 'step') = (step IS NOT NULL))
);
CREATE INDEX shipment_events_parcel ON shipment_events (carrier, tracking_code, at);
