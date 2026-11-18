package e2e

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

// The console's order page shows the order, its parties and the ticket
// about it, and its listing page the listing, its seller, the member who
// hid it and the report about it; each records what it read of people.
func TestConsoleRecordPages(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	order := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID).Order
	tk := openTicket(t, buyer.AccessToken, map[string]any{
		"kind": "not_received", "order_id": order.ID, "body": "GHTK báo đã giao mà mình chưa nhận",
	}, http.StatusCreated)
	other := activeListing(t, seller.AccessToken, nil)
	reporter, _ := newAccount(t)
	expectStatus(t, call(t, "POST", "/v1/reports", map[string]any{"listing_id": other.ID, "reason": "scam", "note": "Đòi cọc"},
		bearer(reporter.AccessToken)), http.StatusCreated)
	lan := signInMember(t, consoledomain.RoleModerator)

	o := expect[struct {
		ID       string                           `json:"id"`
		Status   string                           `json:"status"`
		Listing  struct{ ID, Category string }    `json:"listing"`
		Buyer    struct{ ID, DisplayName string } `json:"buyer"`
		Seller   struct{ ID string }              `json:"seller"`
		Shipment *struct {
			TrackingCode string `json:"tracking_code"`
		} `json:"shipment"`
		Tickets []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			LastLine string `json:"last_line"`
		} `json:"tickets"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/orders/"+order.ID, nil), 200)
	if o.ID != order.ID || o.Status != "shipped" || o.Listing.ID != l.ID || o.Listing.Category == "" || o.Buyer.ID != buyer.Account.ID ||
		o.Seller.ID != seller.Account.ID || o.Shipment == nil || len(o.Tickets) != 1 || o.Tickets[0].ID != tk.ID ||
		o.Tickets[0].Kind != "not_received" {
		t.Fatalf("order %+v", o)
	}
	check(t, "an unknown order", consoleCall(t, lan, "GET", "/v1/console/orders/"+newID(), nil), 404, "order_not_found")
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/orders/"+order.ID, nil), 401)

	minh := signInMember(t, consoledomain.RoleAdmin)
	expectStatus(t, consoleCall(t, minh, "POST", "/v1/console/listings/"+other.ID+"/hide", nil), 200)
	got := expect[struct {
		ID     string              `json:"id"`
		Status string              `json:"status"`
		Seller struct{ ID string } `json:"seller"`
		Hidden *struct {
			ByMemberName string `json:"by_member_name"`
		} `json:"hidden"`
		ReportCaseID *string `json:"report_case_id"`
		Reports      []struct {
			ReporterDisplayName string `json:"reporter_display_name"`
			Reason, Note        string
		} `json:"reports"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/listings/"+other.ID, nil), 200)
	if got.ID != other.ID || got.Status != "hidden" || got.Seller.ID != seller.Account.ID || got.Hidden == nil ||
		got.Hidden.ByMemberName == "" || got.ReportCaseID == nil || *got.ReportCaseID != seller.Account.ID || len(got.Reports) != 1 ||
		got.Reports[0].Note != "Đòi cọc" {
		t.Fatalf("listing %+v", got)
	}
	if plain := expect[struct {
		Hidden  any   `json:"hidden"`
		Reports []any `json:"reports"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/listings/"+l.ID, nil), 200); plain.Hidden != nil || len(plain.Reports) != 0 {
		t.Errorf("the ordered listing %+v", plain)
	}
	check(t, "an unknown listing", consoleCall(t, lan, "GET", "/v1/console/listings/"+newID(), nil), 404, "listing_not_found")
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/listings/"+l.ID, nil), 401)

	counts, err := postgres.Counts[string](db.Query(context.Background(), `SELECT tool_name, count(*) FROM console_agent_reads
WHERE member_id = @member GROUP BY tool_name`, pgx.StrictNamedArgs{"member": lan.ID}))
	if err != nil || counts["console_order"] != 1 || counts["console_listing"] != 1 {
		t.Errorf("reads %v %v", counts, err)
	}
}
