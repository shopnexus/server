package orderbiz

import (
	"context"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// mustNumericOne returns a pgtype.Numeric with value 1 — the identity exchange
// rate used for same-currency wallet transactions where no FX conversion is
// involved. Lifted out of the legacy checkout handler so workflow code paths
// (checkout / confirm / payout) and refund/reject helpers share one definition.
func mustNumericOne() pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan("1")
	return n
}

// markSessionAndTxsFailed is the standard saga-compensator body for workflows
// that create a payment_session + child transactions in Pending state. Both
// underlying queries guard on status='Pending', so this is idempotent and safe
// for saga retries on already-final rows. Shared by checkout / confirm / payout.
func markSessionAndTxsFailed(
	ctx context.Context,
	q orderdb.Querier,
	sessionID uuid.UUID,
	txIDs []uuid.UUID,
	reason string,
) error {
	if _, e := q.MarkPaymentSessionFailed(ctx, sessionID); e != nil {
		return sharedmodel.WrapErr("mark session failed", e)
	}
	if e := q.MarkTransactionsFailed(ctx, orderdb.MarkTransactionsFailedParams{
		ID:    txIDs,
		Error: null.StringFrom(reason),
	}); e != nil {
		return sharedmodel.WrapErr("mark txs failed", e)
	}
	return nil
}
