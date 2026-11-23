package e2e

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/platform/testenv"
)

func TestScamWarning(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	c := openChat(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, "Xe còn không bạn?")
	sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Còn bạn, chuyển cọc qua Zalo mình giữ xe nhé"})

	var row inboxRow
	testenv.Eventually(t, 30*time.Second, func() error {
		if row = inboxRows(t, buyer.AccessToken)[seller.Account.ID]; row.ScamWarning == nil {
			return errors.New("no warning yet")
		}
		return nil
	})
	got := row.ScamWarning.codes()
	for _, want := range []string{"prepayment_request", "off_app_contact", "new_account"} {
		if !slices.Contains(got, want) {
			t.Fatalf("codes %v lack %s", got, want)
		}
	}
	if row.ScamWarning.Dismissed || row.ScamWarning.Signals[0].Text == "" {
		t.Fatalf("%+v", row.ScamWarning)
	}
	for _, s := range row.ScamWarning.Signals {
		if (s.Code == "new_account") != (s.Params["days"] >= 1) {
			t.Fatalf("%s params %v", s.Code, s.Params)
		}
	}
	chatPath := "/v1/screens/chat/" + c.ID
	if v := expect[chatScreen](t, call(t, "GET", chatPath, nil, bearer(buyer.AccessToken)), http.StatusOK); v.ScamWarning == nil ||
		v.ScamWarning.Dismissed {
		t.Fatalf("chat: %+v", v.ScamWarning)
	}
	if theirs := inboxRows(t, seller.AccessToken)[buyer.Account.ID]; theirs.ScamWarning != nil {
		t.Fatalf("the suspect sees %+v", theirs.ScamWarning)
	}

	path := "/v1/conversations/" + c.ID + "/scam-warning/dismiss"
	check(t, "dismiss", call(t, "POST", path, nil, bearer(buyer.AccessToken)), 204, "")
	check(t, "dismiss again", call(t, "POST", path, nil, bearer(buyer.AccessToken)), 204, "")
	if v := expect[chatScreen](t, call(t, "GET", chatPath, nil, bearer(buyer.AccessToken)), http.StatusOK); !v.ScamWarning.Dismissed {
		t.Fatalf("after the dismissal: %+v", v.ScamWarning)
	}
	if r := inboxRows(t, buyer.AccessToken)[seller.Account.ID]; r.ScamWarning == nil || !r.ScamWarning.Dismissed {
		t.Fatalf("the inbox keeps the warning, dismissed: %+v", r.ScamWarning)
	}

	sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Gửi mình mã OTP để xác nhận đơn"})
	testenv.Eventually(t, 30*time.Second, func() error {
		v := expect[chatScreen](t, call(t, "GET", chatPath, nil, bearer(buyer.AccessToken)), http.StatusOK)
		if v.ScamWarning.Dismissed || !slices.Contains(v.ScamWarning.codes(), "sensitive_info_request") {
			return fmt.Errorf("still %+v", v.ScamWarning)
		}
		return nil
	})

	check(t, "dismiss without a warning", call(t, "POST", path, nil, bearer(seller.AccessToken)), 204, "")
	check(t, "dismiss signed out", call(t, "POST", path, nil), 401, "unauthenticated")
	check(t, "dismiss a bad id", call(t, "POST", "/v1/conversations/x/scam-warning/dismiss", nil, bearer(buyer.AccessToken)), 400, "")
	stranger, _ := newAccount(t)
	check(t, "dismiss someone else's", call(t, "POST", path, nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")
}
