package orderbiz

import (
	"github.com/jackc/pgx/v5/pgtype"
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
