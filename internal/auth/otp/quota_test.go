package otp_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/auth/otp"
)

var destRules = []otp.Rule{{Window: 45 * time.Second, Max: 1}, {Window: time.Hour, Max: 5}}

func TestQuotaSpacing(t *testing.T) {
	q := otp.NewQuota("dest:+84901234567")
	if _, _, ok := q.Admit(now, destRules...); !ok {
		t.Fatal("first send refused")
	}
	rule, wait, ok := q.Admit(now.Add(44*time.Second), destRules...)
	if ok || rule.Window != 45*time.Second || wait != time.Second {
		t.Fatalf("at 44 s: %v %v %v", rule, wait, ok)
	}
	if _, _, ok := q.Admit(now.Add(45*time.Second), destRules...); !ok {
		t.Fatal("at 45 s refused")
	}
}

func TestQuotaHourlyCap(t *testing.T) {
	q := otp.NewQuota("dest:+84901234567")
	at := now
	for i := range 5 {
		if _, _, ok := q.Admit(at, destRules...); !ok {
			t.Fatalf("send %d refused", i+1)
		}
		at = at.Add(45 * time.Second)
	}
	if rule, _, ok := q.Admit(at, destRules...); ok || rule.Window != time.Hour {
		t.Fatalf("sixth send: %v %v", rule, ok)
	}
	if _, _, ok := q.Admit(now.Add(time.Hour+time.Second), destRules...); !ok {
		t.Fatal("an hour after the first send refused")
	}
	if len(q.SentAt()) > 5 {
		t.Fatalf("sends older than the longest window kept: %d", len(q.SentAt()))
	}
}

func TestQuotaJSONRoundTrip(t *testing.T) {
	q := otp.NewQuota("dest:+84901234567")
	q.Admit(now, otp.Rule{Window: time.Hour, Max: 5})
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var back otp.Quota
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, q) {
		t.Fatalf("got %+v, want %+v", back, q)
	}
}
