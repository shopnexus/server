package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// retryListen is the pause before Listen connects again after a failure.
const retryListen = time.Second

// Listen calls on with the payload of every notification on channel until
// ctx is done. It holds a connection of its own, outside the pool: a pooled
// one would hold a slot for good, and Pool.Close would wait for it. A lost
// connection is logged and opened again after a pause; notifications sent
// meanwhile are lost. channel goes into the SQL unquoted, so it must be a
// constant of the caller.
func Listen(ctx context.Context, pool *pgxpool.Pool, channel string, log *slog.Logger, on func(payload string)) {
	for ctx.Err() == nil {
		if err := listenOnce(ctx, pool, channel, on); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "postgres: listen", "channel", channel, "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(retryListen):
			}
		}
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, channel string, on func(payload string)) error {
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		// Closing a listen connection that already failed reports that
		// failure again; Listen has it from WaitForNotification.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	for {
		n, waitErr := conn.WaitForNotification(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait for notification: %w", waitErr)
		}
		on(n.Payload)
	}
}
