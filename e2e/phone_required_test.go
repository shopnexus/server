package e2e

import (
	"net/http"
	"testing"
)

// An account that signed up by email adds a phone before it trades.
func TestPhoneRequiredToTrade(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	me, _ := newEmailAccount(t)
	auth := bearer(me.AccessToken)
	check(t, "post", call(t, "POST", "/v1/listings", draftWith(upload(t, me.AccessToken, photoWithGPS(t))), auth, newKey()),
		http.StatusForbidden, "phone_required")
	check(t, "open a deal", call(t, "POST", "/v1/deals", map[string]any{"listing_id": l.ID}, auth), http.StatusForbidden, "phone_required")
	check(t, "read a video", call(t, "POST", "/v1/video-reads", map[string]any{"video": uploadVideo(t, me.AccessToken, "a video")}, auth, newKey()),
		http.StatusForbidden, "phone_required")
	check(t, "read a link", call(t, "POST", "/v1/link-reads", map[string]any{"url": iphoneURL(iphoneListing)}, auth, newKey()),
		http.StatusForbidden, "phone_required")
	check(t, "open a chat", call(t, "POST", "/v1/conversations", map[string]any{"listing_id": l.ID, "text": "Còn không?"}, auth, newKey()),
		http.StatusForbidden, "phone_required")
	expectStatus(t, call(t, "POST", "/v1/accounts/me/identities", map[string]string{"phone_token": issuer.SMSToken(newPhone())}, newKey(), auth), 200)
	postListing(t, me.AccessToken, nil)
	openDeal(t, me.AccessToken, l.ID, http.StatusCreated)
}
