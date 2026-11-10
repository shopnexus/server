package e2e

import (
	"testing"
	"time"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
)

func TestDeleteMyAccount(t *testing.T) {
	t.Parallel()
	me, phone := newAccount(t)

	p, _ := accountdomain.NewVerifier(keys).Verify(me.AccessToken, time.Now())
	s, _ := accountdomain.NewSession(p.SessionID, p.AccountID, "", time.Now().Add(-20*time.Minute), time.Now())
	stale, _, _ := accountdomain.NewSigner(keys).Sign(s, time.Now())
	check(t, "stale sign-in", call(t, "DELETE", "/v1/accounts/me", nil, bearer(stale)), 428, "reauth_required")
	check(t, "signed out", call(t, "DELETE", "/v1/accounts/me", nil), 401, "unauthenticated")

	check(t, "delete", call(t, "DELETE", "/v1/accounts/me", nil, bearer(me.AccessToken)), 204, "")
	check(t, "refresh", refresh(t, me.RefreshToken), 401, "session_revoked")
	again := signInWith(t, phone)
	if again.Account.ID == me.Account.ID || !again.Account.IsNew {
		t.Fatalf("the phone signed in to %s, new %t", again.Account.ID, again.Account.IsNew)
	}
}

func TestDeleteMyAccountRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	check(t, "open order", call(t, "DELETE", "/v1/accounts/me", nil, bearer(seller.AccessToken)), 409, "orders_open")
	check(t, "open order, buyer", call(t, "DELETE", "/v1/accounts/me", nil, bearer(buyer.AccessToken)), 409, "orders_open")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", call(t, "DELETE", "/v1/accounts/me", nil, bearer(suspended.AccessToken)), 403, "account_suspended")
}
