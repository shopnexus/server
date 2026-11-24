package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func (s *shipping) Reported(_ context.Context, c carrier.Carrier, code string, st carrier.Status) error {
	s.reported = append(s.reported, string(c)+" "+code+" "+string(st))
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
	handler.New(svc, secrets, slog.New(slog.DiscardHandler)).Register(r, protect)
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

func TestGHTKReportAsFormAndJSON(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	form := url.Values{"label_id": {"S1.A1.2001297581"}, "partner_id": {"SN-100001"}, "status_id": {"5"}, "reason_code": {""}}
	if w := call(t, r, "POST", "/v1/carriers/ghtk/webhook/s3cret", "application/x-www-form-urlencoded", form.Encode()); w.Code != http.StatusOK {
		t.Fatalf("form: %d %s", w.Code, w.Body)
	}
	if w := call(t, r, "POST", "/v1/carriers/ghtk/webhook/s3cret", "application/json",
		`{"label_id":"S1.A1.2001297581","partner_id":"SN-100001","status_id":3,"action_time":"2026-11-13T09:00:00+07:00"}`); w.Code != http.StatusOK {
		t.Fatalf("json: %d %s", w.Code, w.Body)
	}
	want := []string{"ghtk S1.A1.2001297581 delivered", "ghtk S1.A1.2001297581 in_transit"}
	if strings.Join(svc.reported, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", svc.reported)
	}
}

// Only switch_status moves a GHN shipment; a fee or COD event's Status may be stale.
func TestGHNReportMovesOnlyOnSwitchStatus(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := httpx.NewRouter()
	handler.New(svc, map[carrier.Carrier]string{carrier.GHN: "g1"}, slog.New(slog.DiscardHandler)).Register(r, func(h http.Handler) http.Handler { return h })
	for _, body := range []string{
		`{"OrderCode":"LY9VHQ","Type":"switch_status","Status":"picked","ShopID":241112}`,
		`{"OrderCode":"LY9VHQ","Type":"update_fee","Status":"picked","TotalFee":20900}`,
		`{"OrderCode":"LY9VHQ","Type":"cod","Status":"delivered"}`,
		`{"OrderCode":"LY9VHQ","Type":"Switch_status","Status":"delivered"}`,
	} {
		if w := call(t, r, "POST", "/v1/carriers/ghn/webhook/g1", "application/json", body); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	want := []string{"ghn LY9VHQ in_transit", "ghn LY9VHQ delivered"}
	if strings.Join(svc.reported, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", svc.reported)
	}
}

func TestAShipperNoticeOrAnUnreadableReportMovesNothing(t *testing.T) {
	t.Parallel()
	svc := &shipping{}
	r := router(svc)
	for _, body := range []string{`{"label_id":"S1","status_id":123}`, `{"label_id":"S1","status_id":-1}`, `{"status_id":5}`, `not json`} {
		if w := call(t, r, "POST", "/v1/carriers/ghtk/webhook/s3cret", "application/json", body); w.Code != http.StatusOK {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	if len(svc.reported) != 0 {
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
