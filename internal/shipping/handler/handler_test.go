package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/handler"
	"github.com/shopnexus/server/internal/shipping/service"
)

// shipping records what the handler asks of the service.
type shipping struct {
	service.Service

	reported []string
	linked   []carrier.Credentials
	unlinked []string
	texted   []string
}

func (s *shipping) SendOTP(_ context.Context, c, phone string) error {
	s.texted = append(s.texted, c+" "+phone)
	return nil
}

// Reported records "carrier media body"; a body "unread" does not read.
func (s *shipping) Reported(_ context.Context, c carrier.Carrier, contentType string, body []byte) error {
	if string(body) == "unread" {
		return domain.ErrReportUnread
	}
	s.reported = append(s.reported, string(c)+" "+contentType+" "+string(body))
	return nil
}

func (s *shipping) DevReported(_ context.Context, code string, ev carrier.Event) error {
	s.reported = append(s.reported, "dev "+code+" "+string(ev.Step)+" "+ev.Reason)
	return nil
}

func (s *shipping) Link(_ context.Context, _ uuid.UUID, c string, cr carrier.Credentials) (service.AccountView, error) {
	s.linked = append(s.linked, cr)
	return service.AccountView{Carrier: c, ShopCode: "S19159", LinkedAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}, nil
}

func (s *shipping) Unlink(_ context.Context, _ uuid.UUID, c string) error {
	if c == "ghn" {
		return domain.ErrShipmentsOpen
	}
	s.unlinked = append(s.unlinked, c)
	return nil
}

func router(svc *shipping) *httpx.Router {
	r := httpx.NewRouter()
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(authn.With(req.Context(), authn.Principal{AccountID: uuid.NewV7()})))
		})
	}
	secrets := map[carrier.Carrier]string{carrier.GHTK: "s3cret", carrier.GHN: ""}
	handler.New(svc, secrets, slog.New(slog.DiscardHandler)).Register(r, protect, true)
	return r
}

func call(t *testing.T, r http.Handler, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The webhook hands the carrier's report to the service as it came, and
// answers 200 to one that does not read, so the carrier does not resend.
func TestTheWebhookPassesTheReportOn(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	for _, body := range []string{"label_id=S1&status_id=5", "unread"} {
		if w := call(t, r, "POST", "/v1/carriers/ghtk/webhook/s3cret", "application/x-www-form-urlencoded", body); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if len(svc.reported) != 1 || svc.reported[0] != "ghtk application/x-www-form-urlencoded label_id=S1&status_id=5" {
		t.Fatalf("%v", svc.reported)
	}
}

func TestTheDevRouteTakesAStep(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	if w := call(t, r, "POST", "/v1/dev/shipments/GHN-1/status", "application/json", `{"step":"delivery_failed","reason":"Không liên lạc được"}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(svc.reported) != 1 || svc.reported[0] != "dev GHN-1 delivery_failed Không liên lạc được" {
		t.Fatalf("%v", svc.reported)
	}
}

func TestAWrongSecretOrCarrierIs404(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	for _, path := range []string{"/v1/carriers/ghtk/webhook/nope", "/v1/carriers/spx/webhook/s3cret", "/v1/carriers/ghn/webhook/"} {
		if w := call(t, r, "POST", path, "application/json", `{"label_id":"S1","status_id":5}`); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	if len(svc.reported) != 0 {
		t.Fatalf("%v", svc.reported)
	}
}

func TestLinkTakesTheCarriersOwnBody(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	w := call(t, r, "PUT", "/v1/carrier-accounts/ghtk", "application/json", `{"email":"a@b.vn","password":"secret"}`)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); w.Code != http.StatusOK || err != nil || body["shop_code"] != "S19159" || len(body) != 3 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w = call(t, r, "PUT", "/v1/carrier-accounts/ghtk", "application/json", `{"token":"t","shop_id":"885"}`); w.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(w.Body.String(), "invalid_carrier") {
		t.Fatalf("GHN's body for GHTK: %d %s", w.Code, w.Body)
	}
	if w = call(t, r, "PUT", "/v1/carrier-accounts/ghn", "application/json", `{"phone":"0356875408","otp":"860228"}`); w.Code != http.StatusOK {
		t.Fatalf("GHN by a code: %d %s", w.Code, w.Body)
	}
	if w = call(t, r, "PUT", "/v1/carrier-accounts/ghtk", "application/json", `{"email":"a@b.vn","password":"x","otp":"1"}`); w.Code < 400 {
		t.Fatalf("GHN's code for GHTK: %d %s", w.Code, w.Body)
	}
	want := []carrier.Credentials{{Email: "a@b.vn", Password: "secret"}, {Phone: "0356875408", OTP: "860228"}}
	if len(svc.linked) != 2 || svc.linked[0] != want[0] || svc.linked[1] != want[1] {
		t.Fatalf("%+v", svc.linked)
	}
}

func TestSendOTP(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	if w := call(t, r, "POST", "/v1/carrier-accounts/ghn/otp", "application/json", `{"phone":"0356875408"}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(svc.texted) != 1 || svc.texted[0] != "ghn 0356875408" {
		t.Fatalf("%+v", svc.texted)
	}
}

func TestUnlink(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	if w := call(t, r, "DELETE", "/v1/carrier-accounts/ghtk", "", ""); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := call(t, r, "DELETE", "/v1/carrier-accounts/ghn", "", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "shipments_open") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
