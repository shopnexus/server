package e2e

import "testing"

func TestOTPSendDeliversACode(t *testing.T) {
	phone := newPhone()
	res := sendOTP(t, "phone", phone)
	if c := decode[challenge](t, res); res.Status != 200 || c.ChallengeID == "" || c.ResendAfter != 45 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	if box.code(t, phone, 1) == "" {
		t.Fatal("the message carries no code")
	}
}

func TestOTPSendEmail(t *testing.T) {
	addr := newEmail()
	if res := sendOTP(t, "email", addr); res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	box.code(t, addr, 1)
}

func TestOTPSendTooSoon(t *testing.T) {
	phone := newPhone()
	sendOTP(t, "phone", phone)
	res := sendOTP(t, "phone", phone)
	if e := errorOf(t, res); res.Status != 429 || e.Code != "otp_too_soon" || e.Details["retry_after"] == nil {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendLimitPerAddress(t *testing.T) {
	ip := newIP()
	for i := range 20 {
		if res := sendOTP(t, "phone", newPhone(), ip); res.Status != 200 {
			t.Fatalf("send %d: %d %s", i+1, res.Status, res.Body)
		}
	}
	if res := sendOTP(t, "phone", newPhone(), ip); res.Status != 429 || errorOf(t, res).Code != "otp_limit" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendRejectsALandline(t *testing.T) {
	if res := sendOTP(t, "phone", "0201234567"); res.Status != 422 || errorOf(t, res).Code != "invalid_phone" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendRejectsBadRequests(t *testing.T) {
	res := call(t, server, "POST", "/v1/auth/otp",
		map[string]string{"kind": "phone", "destination": newPhone(), "purpose": "add_identity"}, newIP(), newKey())
	if e := errorOf(t, res); res.Status != 400 || e.Fields["purpose"] == "" {
		t.Fatalf("unknown purpose: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "POST", "/v1/auth/otp",
		map[string]string{"kind": "phone", "destination": newPhone(), "purpose": "login"}, newIP())
	if e := errorOf(t, res); res.Status != 400 || e.Fields["Idempotency-Key"] == "" {
		t.Fatalf("no Idempotency-Key: %d %s", res.Status, res.Body)
	}
}
