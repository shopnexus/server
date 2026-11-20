package domain_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

func TestRedact(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Gọi mình 0912 345 678 nhé":                      "Gọi mình <PHONE> nhé",
		"sdt +84 912.345.678":                            "sdt <PHONE>",
		"zalo 0912345678":                                "zalo <PHONE>",
		"STK 1903 6688 2211 01 Techcombank":              "STK <BANK_NUMBER> Techcombank",
		"thẻ 4111-1111-1111-1111":                        "thẻ <BANK_NUMBER>",
		"mail mình là an.nguyen@gmail.com":               "mail mình là <EMAIL>",
		"vào https://www.ghn-check.top/pay?id=9 để nhận": "vào <URL:ghn-check.top> để nhận",
		"link bit.ly/3xYz nhé":                           "link <URL:bit.ly> nhé",
		"Giá 1.200.000đ, cọc 500k":                       "Giá 1.200.000đ, cọc 500k",
		"chốt 12000000đ":                                 "chốt 12000000đ",
		"chốt 12 000 000 đ":                              "chốt 12 000 000 đ",
		"Xe còn không bạn?":                              "Xe còn không bạn?",
		"stk 19036688221101 trước nha":                   "stk <BANK_NUMBER> trước nha",
		"ck vào 19036688221101 không":                    "ck vào <BANK_NUMBER> không",
		"số thẻ 4111 1111 1111 1111 đây":                 "số thẻ <BANK_NUMBER> đây",
		"stk 1903 6688 2211 01 k nha":                    "stk <BANK_NUMBER> k nha",
		"stk 1903.6688.2211.01":                          "stk <BANK_NUMBER>",
		"thẻ 4111.1111.1111.1111":                        "thẻ <BANK_NUMBER>",
		"stk1903668822110":                               "stk<BANK_NUMBER>",
		"zalo0912345678":                                 "zalo<PHONE>",
		"chốt 12.000.000 vnđ nhé":                        "chốt 12.000.000 vnđ nhé",
		"bớt 2tr5 được không":                            "bớt 2tr5 được không",
	} {
		if got := domain.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSignalText(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		s    domain.Signal
		want string
	}{
		{domain.Signal{Code: domain.CodePrepayment}, "Đòi chuyển tiền hoặc đặt cọc trước"},
		{domain.Signal{Code: domain.CodeFakeProof}, `Gửi biên lai giả hoặc bảo "chuyển dư, hoàn lại giúp"`},
		{domain.Signal{Code: domain.CodeNewAccount, Params: map[string]int{"days": 2}}, "Tài khoản mới tạo 2 ngày"},
		{domain.Signal{Code: domain.CodePriceBelow, Params: map[string]int{"percent": 40}}, "Giá thấp hơn 40% so với tin tương tự"},
		{domain.Signal{Code: "unknown"}, ""},
	} {
		if got := c.s.Text(); got != c.want {
			t.Errorf("%s: %q, want %q", c.s.Code, got, c.want)
		}
	}
	for _, code := range domain.MessageCodes {
		if (domain.Signal{Code: code}).Text() == "" || !code.FromMessages() {
			t.Errorf("message code %s has no text or is not a message code", code)
		}
	}
	if domain.CodeNewAccount.FromMessages() || domain.CodePriceBelow.FromMessages() {
		t.Error("a fact code counts as a message code")
	}
}

func TestFactSignals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if s, ok := domain.NewAccountSignal(now.Add(-3*time.Hour), now); !ok || s.Params["days"] != 1 {
		t.Fatalf("3 hours: %+v %v", s, ok)
	}
	if s, ok := domain.NewAccountSignal(now.Add(-50*time.Hour), now); !ok || s.Params["days"] != 2 {
		t.Fatalf("50 hours: %+v %v", s, ok)
	}
	if _, ok := domain.NewAccountSignal(now.Add(-7*24*time.Hour), now); ok {
		t.Fatal("7 days is not new")
	}
	if s, ok := domain.PriceBelowSignal(6_000_000, 10_000_000, 5); !ok || s.Params["percent"] != 40 {
		t.Fatalf("40%%: %+v %v", s, ok)
	}
	if s, ok := domain.PriceBelowSignal(6_999_999, 10_000_000, 5); !ok || s.Params["percent"] != 30 {
		t.Fatalf("30%% rounded down: %+v %v", s, ok)
	}
	for name, c := range map[string]struct {
		price, median int64
		size          int
	}{
		"29%":          {7_100_000, 10_000_000, 5},
		"4 samples":    {1_000_000, 10_000_000, 4},
		"no median":    {1_000_000, 0, 5},
		"above median": {12_000_000, 10_000_000, 9},
	} {
		if _, ok := domain.PriceBelowSignal(c.price, c.median, c.size); ok {
			t.Errorf("%s holds", name)
		}
	}
}

func TestVerdictClean(t *testing.T) {
	t.Parallel()
	v, dropped := domain.Verdict{
		A: []domain.Code{domain.CodePrepayment, "bribery", domain.CodePrepayment, domain.CodeNewAccount},
		B: nil,
	}.Clean()
	if dropped != 3 || len(v.A) != 1 || v.A[0] != domain.CodePrepayment || v.B == nil || len(v.B) != 0 {
		t.Fatalf("%+v %d", v, dropped)
	}
}

func TestWarning(t *testing.T) {
	t.Parallel()
	conversation, victim, suspect := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	newAccount, _ := domain.NewAccountSignal(t0.Add(-time.Hour), t0)
	w := domain.NewWarning(conversation, victim, suspect)

	// Facts alone raise nothing.
	if w.Raise(nil, []domain.Signal{newAccount}, t0, nil) || len(w.Signals()) != 0 {
		t.Fatalf("facts alone: %+v", w.Signals())
	}
	// A message code raises the warning and attaches the fact, both found
	// at the suspect's message.
	message := uuid.NewV7()
	if !w.Raise([]domain.Code{domain.CodeOffApp, domain.CodePrepayment}, []domain.Signal{newAccount}, t0, &message) {
		t.Fatal("first raise changed nothing")
	}
	got := w.Signals()
	if len(got) != 3 || got[0].Code != domain.CodePrepayment || got[1].Code != domain.CodeOffApp ||
		got[2].Code != domain.CodeNewAccount || !got[2].FirstSeenAt.Equal(t0) || w.Dismissed() ||
		*got[0].MessageID != message || *got[2].MessageID != message {
		t.Fatalf("after the first raise: %+v", got)
	}
	// The same codes again change nothing; codes are never removed.
	if w.Raise([]domain.Code{domain.CodePrepayment}, nil, t0.Add(time.Minute), nil) || len(w.Signals()) != 3 {
		t.Fatalf("again: %+v", w.Signals())
	}
	// Dismissing once works, twice changes nothing.
	if !w.Dismiss(t0.Add(2*time.Minute)) || !w.Dismissed() || w.Dismiss(t0.Add(3*time.Minute)) {
		t.Fatal("dismiss")
	}
	// A new fact code does not undo the dismissal.
	price, _ := domain.PriceBelowSignal(5_000_000, 10_000_000, 6)
	if !w.Raise([]domain.Code{domain.CodePrepayment}, []domain.Signal{price}, t0.Add(4*time.Minute), nil) || !w.Dismissed() {
		t.Fatalf("a new fact: dismissed %v, %+v", w.Dismissed(), w.Signals())
	}
	// A new message code does.
	if !w.Raise([]domain.Code{domain.CodeSensitive}, nil, t0.Add(5*time.Minute), nil) || w.Dismissed() {
		t.Fatalf("a new message code: dismissed %v", w.Dismissed())
	}
	last := w.Signals()[len(w.Signals())-1]
	if last.Code != domain.CodeSensitive || !last.FirstSeenAt.Equal(t0.Add(5*time.Minute)) || !w.UpdatedAt().Equal(last.FirstSeenAt) ||
		last.MessageID != nil || *w.Signals()[0].MessageID != message {
		t.Fatalf("last: %+v, updated %v", last, w.UpdatedAt())
	}
	// State and Restore round-trip.
	back := domain.RestoreWarning(w.State())
	if len(back.Signals()) != len(w.Signals()) || back.Dismissed() != w.Dismissed() || back.SuspectID() != suspect {
		t.Fatalf("restore: %+v", back.State())
	}
	// An empty warning cannot be dismissed.
	empty := domain.NewWarning(conversation, suspect, victim)
	if empty.Dismiss(t0) {
		t.Fatal("dismissed an empty warning")
	}
}

// A dismissal during a check, after the check read its clock and before it
// saved, does not hide the code the check found: the code is seen after
// the dismissal.
func TestWarningRaisedAfterADismissalMidCheck(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	w := domain.NewWarning(uuid.NewV7(), uuid.NewV7(), uuid.NewV7())
	w.Raise([]domain.Code{domain.CodeOffApp}, nil, t0, nil)
	checkStarted := t0.Add(time.Minute)
	w.Dismiss(checkStarted.Add(3 * time.Second))
	if !w.Raise([]domain.Code{domain.CodeSensitive}, nil, checkStarted, nil) || w.Dismissed() {
		t.Fatalf("the new code is hidden: %+v", w.State())
	}
	if got := w.Signals()[1].FirstSeenAt; !got.After(*w.DismissedAt()) {
		t.Fatalf("first seen %v, dismissed %v", got, *w.DismissedAt())
	}
}
