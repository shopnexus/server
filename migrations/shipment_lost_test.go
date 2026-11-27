package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// 0104 lets a failed order keep a lost shipment, and only a failed one.
//
// UC-DEAL-05 E4.
func TestShipmentLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	manual := new("manual")
	for name, c := range map[string]struct {
		row  orderRow
		want string
	}{
		"failed":  {orderRow{status: "failed", price: 1_500_000, mode: manual, shipped: &at, closed: &at}, ""},
		"shipped": {orderRow{status: "shipped", price: 1_500_000, mode: manual, shipped: &at}, "orders_shipment_lost_check"},
	} {
		id, err := insertOrder(ctx, t, db, buyer, seller, c.row)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(ctx, `UPDATE orders SET shipment_status = 'lost' WHERE id = @id`, pgx.NamedArgs{"id": id})
		if (c.want == "" && err != nil) || (c.want != "" && !broke(err, c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}
