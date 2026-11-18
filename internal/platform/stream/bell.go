package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres"
)

// channel is the Postgres channel rings travel on.
const channel = "stream"

// Target is an account a ring is for, with the seq its update took in the
// account's log; 0 for a typing ring, which has no update.
type Target struct {
	Account uuid.UUID `json:"account"`
	Seq     int64     `json:"seq,omitempty"`
}

// Bell carries rings to every replica. The update log holds the truth; a
// ring only says that something new is there, so a lost ring costs a read
// of the log, never an update.
type Bell interface {
	// Ring sends h to targets. With tx it rings when tx commits and never on
	// rollback; with nil it rings at once.
	Ring(ctx context.Context, tx pgx.Tx, targets []Target, h Hint) error
	// Listen calls deliver for each ring until ctx is done, and resync each
	// time it starts to listen, since rings sent before then are lost.
	Listen(ctx context.Context, deliver func([]Target, Hint), resync func())
}

// PGBell rings on Postgres channel stream with pg_notify: every replica
// hears every ring and hands it to its own connections. Ring with a
// transaction needs neither field.
type PGBell struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

var _ Bell = PGBell{}

// ring is a ring as it travels on the channel.
type ring struct {
	Targets []Target `json:"targets"`
	Hint    Hint     `json:"hint"`
}

func (b PGBell) Ring(ctx context.Context, tx pgx.Tx, targets []Target, h Hint) error {
	payload, err := json.Marshal(ring{Targets: targets, Hint: h})
	if err != nil {
		return fmt.Errorf("encode ring: %w", err)
	}
	var db postgres.Execer = b.DB
	if tx != nil {
		db = tx
	}
	if _, err = db.Exec(ctx, `SELECT pg_notify(@channel, @payload)`,
		pgx.StrictNamedArgs{"channel": channel, "payload": string(payload)}); err != nil {
		return fmt.Errorf("ring: %w", err)
	}
	return nil
}

func (b PGBell) Listen(ctx context.Context, deliver func([]Target, Hint), resync func()) {
	postgres.Listen(ctx, b.DB, channel, b.Log, resync, func(payload string) {
		var r ring
		if err := json.Unmarshal([]byte(payload), &r); err != nil {
			b.Log.WarnContext(ctx, "stream: bad ring", "err", err)
			return
		}
		deliver(r.Targets, r.Hint)
	})
}
