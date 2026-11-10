package e2e

import (
	"net/http"
	"strings"
	"testing"
	"uuid"
)

type report struct {
	ID             string `json:"id"`
	ListingID      string `json:"listing_id"`
	AccountID      string `json:"account_id"`
	ConversationID string `json:"conversation_id"`
	Reason         string `json:"reason"`
	Note           string `json:"note"`
}

func TestReports(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	reporter, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	send := func(token string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/reports", body, bearer(token))
	}

	first := expect[report](t, send(reporter.AccessToken, map[string]any{"listing_id": l.ID, "reason": "fake"}), http.StatusCreated)
	if first.AccountID != seller.Account.ID || first.ListingID != l.ID || first.Reason != "fake" {
		t.Fatalf("first: %+v", first)
	}
	again := expect[report](t, send(reporter.AccessToken, map[string]any{"listing_id": l.ID, "reason": "scam", "note": " Đòi cọc "}), http.StatusOK)
	if again.ID != first.ID || again.Reason != "scam" || again.Note != "Đòi cọc" {
		t.Fatalf("again: %+v", again)
	}
	person := expect[report](t, send(reporter.AccessToken, map[string]any{"account_id": seller.Account.ID, "reason": "scam"}), http.StatusCreated)
	if person.ListingID != "" || person.AccountID != seller.Account.ID {
		t.Fatalf("account: %+v", person)
	}

	check(t, "signed out", send("", map[string]any{"account_id": seller.Account.ID, "reason": "scam"}), 401, "unauthenticated")
	check(t, "a bad id", send(reporter.AccessToken, map[string]any{"account_id": "x", "reason": "scam"}), 400, "")
	check(t, "no reason", send(reporter.AccessToken, map[string]any{"account_id": seller.Account.ID}), 400, "")
	check(t, "both targets", send(reporter.AccessToken, map[string]any{"listing_id": l.ID, "account_id": seller.Account.ID, "reason": "scam"}), 422, "invalid_report")
	check(t, "an unknown reason", send(reporter.AccessToken, map[string]any{"account_id": seller.Account.ID, "reason": "ugly"}), 422, "invalid_report")
	check(t, "a long note", send(reporter.AccessToken, map[string]any{"account_id": seller.Account.ID, "reason": "other", "note": strings.Repeat("ạ", 501)}), 422, "invalid_report")
	check(t, "oneself", send(reporter.AccessToken, map[string]any{"account_id": reporter.Account.ID, "reason": "scam"}), 422, "self_report")
	check(t, "one's own listing", send(seller.AccessToken, map[string]any{"listing_id": l.ID, "reason": "scam"}), 422, "self_report")
	check(t, "an unknown listing", send(reporter.AccessToken, map[string]any{"listing_id": uuid.NewV7().String(), "reason": "scam"}), 404, "listing_not_found")
	check(t, "an unknown account", send(reporter.AccessToken, map[string]any{"account_id": uuid.NewV7().String(), "reason": "scam"}), 404, "account_not_found")

	gone := activeListing(t, seller.AccessToken, nil)
	expectStatus(t, call(t, "DELETE", "/v1/listings/"+gone.ID, nil, bearer(seller.AccessToken)), http.StatusNoContent)
	check(t, "a deleted listing", send(reporter.AccessToken, map[string]any{"listing_id": gone.ID, "reason": "scam"}), 404, "listing_not_found")

	check(t, "withdraw someone else's", call(t, "DELETE", "/v1/reports/"+first.ID, nil, bearer(seller.AccessToken)), 404, "report_not_found")
	check(t, "withdraw", call(t, "DELETE", "/v1/reports/"+first.ID, nil, bearer(reporter.AccessToken)), 204, "")
	check(t, "withdraw again", call(t, "DELETE", "/v1/reports/"+first.ID, nil, bearer(reporter.AccessToken)), 204, "")
	check(t, "withdraw signed out", call(t, "DELETE", "/v1/reports/"+first.ID, nil), 401, "unauthenticated")
	check(t, "withdraw a bad id", call(t, "DELETE", "/v1/reports/x", nil, bearer(reporter.AccessToken)), 400, "")
	next := expect[report](t, send(reporter.AccessToken, map[string]any{"listing_id": l.ID, "reason": "fake"}), http.StatusCreated)
	if next.ID == first.ID {
		t.Fatal("a report after a withdrawal reused the withdrawn one")
	}

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "a suspended reporter", send(suspended.AccessToken, map[string]any{"account_id": seller.Account.ID, "reason": "scam"}), 403, "account_suspended")
}

// The app blocks, then reports with the conversation; another pair's
// conversation is refused.
func TestReportFromAConversation(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	reporter, _ := newAccount(t)
	other, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	c := openChat(t, reporter.AccessToken, l.ID, "Chuyển khoản trước nhé?")
	theirs := openChat(t, other.AccessToken, l.ID, "Còn không bạn?")
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": seller.Account.ID}, bearer(reporter.AccessToken)), http.StatusNoContent)
	got := expect[report](t, call(t, "POST", "/v1/reports", map[string]any{
		"account_id": seller.Account.ID, "reason": "scam", "conversation_id": c.ID,
	}, bearer(reporter.AccessToken)), http.StatusCreated)
	if got.ConversationID != c.ID {
		t.Fatalf("report: %+v", got)
	}
	check(t, "another pair's conversation", call(t, "POST", "/v1/reports", map[string]any{
		"account_id": seller.Account.ID, "reason": "scam", "conversation_id": theirs.ID,
	}, bearer(reporter.AccessToken)), 422, "invalid_report")
	check(t, "a conversation beside a listing", call(t, "POST", "/v1/reports", map[string]any{
		"listing_id": l.ID, "reason": "scam", "conversation_id": c.ID,
	}, bearer(reporter.AccessToken)), 422, "invalid_report")
}
