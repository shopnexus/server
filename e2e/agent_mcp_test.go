package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"uuid"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

// headers sets the agent's headers on every request to the MCP server.
type headers struct {
	token, member string
}

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.token)
	r.Header.Set("X-Console-Member", h.member)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects to the server's MCP endpoint as the agent does, for
// member with token.
func mcpSession(t *testing.T, token, member string) (*sdk.ClientSession, error) {
	t.Helper()
	c := sdk.NewClient(&sdk.Implementation{Name: "e2e-agent", Version: "1"}, nil)
	s, err := c.Connect(t.Context(), &sdk.StreamableClientTransport{
		Endpoint: mcpURL, HTTPClient: &http.Client{Transport: headers{token: token, member: member}}, DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, err
}

// callTool calls a tool, checks its structured result against the
// tool's output schema, and decodes it into T.
func callTool[T any](t *testing.T, s *sdk.ClientSession, schemas map[string]*jsonschema.Resolved, name string, args map[string]any) T {
	t.Helper()
	res, err := s.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		b, _ := json.Marshal(res.Content)
		t.Fatalf("%s: tool error %s", name, b)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var instance any
	if err = json.Unmarshal(b, &instance); err != nil {
		t.Fatal(err)
	}
	if err = schemas[name].Validate(instance); err != nil {
		t.Fatalf("%s: the result breaks the output schema: %v\n%s", name, err, b)
	}
	// The agent reads the result, and the ids of its sources, from the text.
	var text any
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d content blocks, want the JSON text", name, len(res.Content))
	}
	if tc, ok := res.Content[0].(*sdk.TextContent); !ok || json.Unmarshal([]byte(tc.Text), &text) != nil || !reflect.DeepEqual(text, instance) {
		t.Fatalf("%s: the text content is not the result's JSON: %#v", name, res.Content[0])
	}
	var out T
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// reads lists the tools whose reads of personal data were recorded for
// member.
func reads(t *testing.T, member string) map[string][]uuid.UUID {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT tool_name, subject_ids FROM console_agent_reads WHERE member_id = @member`,
		pgx.StrictNamedArgs{"member": uuid.MustParse(member)})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]uuid.UUID{}
	for rows.Next() {
		var tool string
		var ids []uuid.UUID
		if err = rows.Scan(&tool, &ids); err != nil {
			t.Fatal(err)
		}
		out[tool] = append(out[tool], ids...)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Every tool reads for an active member only, declares that it only
// reads, answers within its output schema and records what it read of
// people. UC-MOD-08.
func TestAgentMCPTools(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	putToken(t, buyer.AccessToken, "phone-"+buyer.Account.ID)
	l := activeListing(t, seller.AccessToken, nil)
	order := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID).Order
	tk := openTicket(t, buyer.AccessToken, map[string]any{
		"kind": "not_received", "order_id": order.ID, "text": "GHTK báo đã giao mà mình chưa nhận",
	}, http.StatusCreated)
	reporter, _ := newAccount(t)
	expectStatus(t, call(t, "POST", "/v1/reports", map[string]any{"listing_id": l.ID, "reason": "scam", "note": "Đòi cọc"},
		bearer(reporter.AccessToken)), http.StatusCreated)
	id, _, _ := newMember(t, consoledomain.RoleModerator)

	s, err := mcpSession(t, agentMCPToken, id)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	schemas := map[string]*jsonschema.Resolved{}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Title == "" || tool.InputSchema == nil ||
			tool.OutputSchema == nil {
			t.Errorf("%s: read-only %v, schemas %v %v", tool.Name, tool.Annotations, tool.InputSchema, tool.OutputSchema)
			continue
		}
		b, _ := json.Marshal(tool.OutputSchema)
		var schema jsonschema.Schema
		if err = json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}
		if schemas[tool.Name], err = schema.Resolve(nil); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
	}
	slices.Sort(names)
	if want := []string{
		"account_get", "accounts_list", "console_actions", "count", "listing_get", "listings_of_account", "lookup",
		"order_get", "orders_of_account", "price_guide", "report_case_get", "report_cases_list", "ticket_get",
		"ticket_messages", "tickets_list", "tickets_of_account",
	}; !slices.Equal(names, want) {
		t.Fatalf("tools %v, want %v", names, want)
	}

	ticket := callTool[struct {
		ID       string               `json:"id"`
		Person   struct{ ID string }  `json:"person"`
		Order    *struct{ ID string } `json:"order"`
		Messages []struct {
			From string `json:"from"`
			Text string `json:"text"`
		} `json:"messages"`
	}](t, s, schemas, "ticket_get", map[string]any{"ticket_id": tk.ID})
	if ticket.ID != tk.ID || ticket.Person.ID != buyer.Account.ID || ticket.Order == nil || ticket.Order.ID != order.ID ||
		len(ticket.Messages) == 0 || ticket.Messages[len(ticket.Messages)-1].From != "person" {
		t.Fatalf("ticket_get %+v", ticket)
	}
	ofSeller := callTool[struct {
		Tickets []struct {
			ID string `json:"id"`
			As string `json:"as"`
		} `json:"tickets"`
	}](t, s, schemas, "tickets_of_account", map[string]any{"account_id": seller.Account.ID})
	if len(ofSeller.Tickets) != 1 || ofSeller.Tickets[0].ID != tk.ID || ofSeller.Tickets[0].As != "seller" {
		t.Fatalf("tickets of the seller %+v", ofSeller)
	}
	account := callTool[struct {
		ID       string `json:"id"`
		HasPhone bool   `json:"has_phone"`
		HasEmail bool   `json:"has_email"`
	}](t, s, schemas, "account_get", map[string]any{"account_id": seller.Account.ID})
	if account.ID != seller.Account.ID || !account.HasPhone || account.HasEmail {
		t.Fatalf("account_get %+v", account)
	}
	callTool[struct {
		Items []any `json:"items"`
	}](t, s, schemas, "console_actions", map[string]any{"target_id": seller.Account.ID})
	callTool[struct {
		Items []any `json:"items"`
	}](t, s, schemas, "console_actions", map[string]any{"limit": 5})
	got := callTool[struct {
		ID       string `json:"id"`
		SellerID string `json:"seller_id"`
		Shipment *struct {
			TrackingCode string `json:"tracking_code"`
		} `json:"shipment"`
	}](t, s, schemas, "order_get", map[string]any{"order_id": order.ID})
	if got.ID != order.ID || got.SellerID != seller.Account.ID || got.Shipment == nil || got.Shipment.TrackingCode == "" {
		t.Fatalf("order_get %+v", got)
	}
	listing := callTool[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}](t, s, schemas, "listing_get", map[string]any{"listing_id": l.ID})
	if listing.ID != l.ID {
		t.Fatalf("listing_get %+v", listing)
	}
	callTool[struct {
		ListingID string `json:"listing_id"`
	}](t, s, schemas, "price_guide", map[string]any{"listing_id": l.ID})

	type counted struct {
		Entity   string         `json:"entity"`
		Total    int            `json:"total"`
		ByStatus map[string]int `json:"by_status"`
	}
	for _, entity := range []string{"accounts", "listings", "orders", "tickets", "report_cases"} {
		n := callTool[counted](t, s, schemas, "count", map[string]any{"entity": entity})
		sum := 0
		for _, v := range n.ByStatus {
			sum += v
		}
		if n.Entity != entity || n.Total < 1 || n.Total != sum {
			t.Fatalf("count %s: %+v", entity, n)
		}
	}
	if n := callTool[counted](t, s, schemas, "count", map[string]any{"entity": "accounts", "since": "2999-01-01T00:00:00Z"}); n.Total != 0 {
		t.Fatalf("accounts from the future: %+v", n)
	}
	if bad, badErr := s.CallTool(t.Context(), &sdk.CallToolParams{Name: "count", Arguments: map[string]any{"entity": "carts"}}); badErr == nil &&
		!bad.IsError {
		t.Fatalf("an unknown entity was counted: %+v", bad)
	}

	type matched struct {
		Matches []struct {
			Kind string `json:"kind"`
		} `json:"matches"`
	}
	for id, want := range map[string][]string{
		seller.Account.ID: {"account", "report_case"}, buyer.Account.ID: {"account"}, order.ID: {"order"}, l.ID: {"listing"},
		tk.ID: {"ticket"}, newID(): nil,
	} {
		var got []string
		for _, m := range callTool[matched](t, s, schemas, "lookup", map[string]any{"id": id}).Matches {
			got = append(got, m.Kind)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("lookup %s: %v, want %v", id, got, want)
		}
	}

	type idOnly struct {
		ID         string `json:"id"`
		OrderTitle string `json:"order_title"`
	}
	type paged struct {
		Items      []idOnly `json:"items"`
		NextCursor *string  `json:"next_cursor"`
	}
	has := func(p paged, id string) bool {
		return slices.ContainsFunc(p.Items, func(x idOnly) bool { return x.ID == id })
	}
	// The queue is every test's, so page through it until the ticket shows.
	args := map[string]any{"kind": "not_received", "limit": 50}
	for {
		q := callTool[paged](t, s, schemas, "tickets_list", args)
		if i := slices.IndexFunc(q.Items, func(x idOnly) bool { return x.ID == tk.ID }); i >= 0 {
			if q.Items[i].OrderTitle == "" {
				t.Fatalf("tickets_list: the ticket has no order_title %+v", q.Items[i])
			}
			break
		}
		if q.NextCursor == nil {
			t.Fatal("tickets_list: the ticket is not in the open queue")
		}
		args["cursor"] = *q.NextCursor
	}
	if msgs := callTool[paged](t, s, schemas, "ticket_messages", map[string]any{"ticket_id": tk.ID, "limit": 1}); len(msgs.Items) != 1 {
		t.Fatalf("ticket_messages %+v", msgs)
	}
	if one := callTool[paged](t, s, schemas, "accounts_list", map[string]any{"q": seller.Account.ID}); len(one.Items) != 1 ||
		!has(one, seller.Account.ID) {
		t.Fatalf("accounts_list by id %+v", one)
	}
	if bought := callTool[paged](t, s, schemas, "orders_of_account", map[string]any{"account_id": buyer.Account.ID}); !has(bought, order.ID) {
		t.Fatalf("orders_of_account as buyer %+v", bought)
	}
	if sold := callTool[paged](t, s, schemas, "orders_of_account",
		map[string]any{"account_id": seller.Account.ID, "as": "seller"}); !has(sold, order.ID) {
		t.Fatalf("orders_of_account as seller %+v", sold)
	}
	if onSale := callTool[paged](t, s, schemas, "listings_of_account", map[string]any{"account_id": seller.Account.ID}); !has(onSale, l.ID) {
		t.Fatalf("listings_of_account %+v", onSale)
	}
	callTool[struct {
		Items []any `json:"items"`
	}](t, s, schemas, "report_cases_list", map[string]any{"limit": 5})
	rc := callTool[struct {
		AccountID string `json:"account_id"`
		Reports   []struct {
			ReporterID string `json:"reporter_id"`
			Note       string `json:"note"`
		} `json:"reports"`
	}](t, s, schemas, "report_case_get", map[string]any{"account_id": seller.Account.ID})
	if rc.AccountID != seller.Account.ID || len(rc.Reports) != 1 || rc.Reports[0].ReporterID != reporter.Account.ID ||
		rc.Reports[0].Note != "Đòi cọc" {
		t.Fatalf("report_case_get %+v", rc)
	}

	res, err := s.CallTool(t.Context(), &sdk.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ticket_id": newID()}})
	if err != nil || !res.IsError {
		t.Fatalf("an unknown ticket: %+v %v", res, err)
	}

	recorded := reads(t, id)
	if !slices.Contains(recorded["ticket_get"], uuid.MustParse(tk.ID)) ||
		!slices.Contains(recorded["ticket_get"], uuid.MustParse(buyer.Account.ID)) ||
		!slices.Contains(recorded["tickets_of_account"], uuid.MustParse(tk.ID)) ||
		!slices.Contains(recorded["tickets_list"], uuid.MustParse(tk.ID)) ||
		!slices.Contains(recorded["ticket_messages"], uuid.MustParse(tk.ID)) ||
		!slices.Contains(recorded["report_case_get"], uuid.MustParse(reporter.Account.ID)) {
		t.Fatalf("reads recorded %v", recorded)
	}
	for _, tool := range []string{
		"account_get", "console_actions", "order_get", "listing_get", "price_guide", "count", "lookup", "accounts_list",
		"orders_of_account", "listings_of_account", "report_cases_list",
	} {
		if len(recorded[tool]) != 0 {
			t.Errorf("%s read no personal data but recorded %v", tool, recorded[tool])
		}
	}
}

// A call without the token or for a member who is not active gets nothing.
func TestAgentMCPRefusals(t *testing.T) {
	t.Parallel()
	id, _, _ := newMember(t, consoledomain.RoleModerator)
	if _, err := mcpSession(t, "not-the-token", id); err == nil {
		t.Error("a wrong token connects")
	}
	if _, err := mcpSession(t, agentMCPToken, "not-a-member"); err == nil {
		t.Error("a member id that is not one connects")
	}
	if _, err := mcpSession(t, agentMCPToken, newID()); err == nil {
		t.Error("an unknown member connects")
	}
	s, err := mcpSession(t, agentMCPToken, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = consoleSvc.DisableMember(context.Background(), uuid.MustParse(id), audit.ByCLI("e2e")); err != nil {
		t.Fatal(err)
	}
	// The session was open; the next call checks the member again.
	if res, callErr := s.CallTool(t.Context(), &sdk.CallToolParams{Name: "account_get", Arguments: map[string]any{"account_id": newID()}}); callErr == nil {
		t.Fatalf("a disabled member's call: %+v", res)
	}
	if _, err = mcpSession(t, agentMCPToken, id); err == nil {
		t.Error("a disabled member connects")
	}
}
