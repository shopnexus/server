package carrier_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/provider/carrier"
)

// ghtkServer plays GHTK with the documentation's sample answers. Every
// route checks the token it is called with.
func ghtkServer(t *testing.T) carrier.GHTKClient {
	t.Helper()
	shop := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Token") != "shop-token" || r.Header.Get("X-Client-Source") != "SNX" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /services/shops/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Token") != "partner" || r.Header.Get("X-Client-Source") != "SNX" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Password != "right" {
			_, _ = w.Write([]byte(`{"success":false,"message":"Dữ liệu đăng nhập không hợp lệ"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"Thành công","data":{"code":"S19159","token":"shop-token"}}`))
	})
	mux.HandleFunc("GET /services/shipment/list_pick_add", func(w http.ResponseWriter, r *http.Request) {
		if shop(w, r) {
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":[{"pick_address_id":"88256","address":"Số 105, Hoàng Mai, Hà Nội","pick_tel":"0987654321","pick_name":"Store 1"}]}`))
		}
	})
	mux.HandleFunc("POST /services/shipment/order", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r) {
			return
		}
		var in struct {
			Products []struct {
				Name     string  `json:"name"`
				Weight   float64 `json:"weight"`
				Quantity int     `json:"quantity"`
			} `json:"products"`
			Order map[string]any `json:"order"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		o := in.Order
		if o["id"] == "SN-BAD" {
			_, _ = w.Write([]byte(`{"success":false,"message":"Số điện thoại người nhận không hợp lệ"}`))
			return
		}
		if o["id"] == "SN-DUP" {
			_, _ = w.Write([]byte(`{"success":false,"message":"Mã đơn hàng đã tồn tại","error":{"code":"ORDER_ID_EXIST","partner_id":"SN-DUP","ghtk_label":"S1.A1.111","created":"2026-11-13 10:00:00","status":2}}`))
			return
		}
		if o["pick_address_id"] != "88256" || o["pick_money"] != float64(47_000) || o["hamlet"] != "Khác" ||
			o["ward"] != "Phường Bến Nghé" || o["province"] != "TP. Hồ Chí Minh" || o["transport"] != "fly" ||
			o["pick_option"] != "cod" || len(in.Products) != 1 || in.Products[0].Weight != 0.8 {
			t.Errorf("order %+v products %+v", o, in.Products)
		}
		// The note to the shipper goes as GHTK's note, and only when there is one.
		if note, has := o["note"]; (o["id"] == "SN-100001" && note != "Địa chỉ công ty: giao giờ hành chính") || (o["id"] == "SN-NONOTE" && has) {
			t.Errorf("order %s note %v", o["id"], note)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","order":{"partner_id":"SN-100001","label":"S1.A1.2001297581","area":"1","fee":"30400","insurance_fee":"0","tracking_id":2001297581,"status_id":2}}`))
	})
	mux.HandleFunc("GET /services/shipment/v2/{code}", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r) {
			return
		}
		if r.PathValue("code") == "S1.LATE" {
			_, _ = w.Write([]byte(`{"success":true,"message":"","order":{"label_id":"S1.LATE","status":"10","customer_fullname":"Vân","address":"1 Lê Lợi","ship_money":"16500","pick_money":47000}}`))
			return
		}
		if r.PathValue("code") != "S1.A1.17373471" {
			_, _ = w.Write([]byte(`{"success":false,"message":"Không tìm thấy đơn hàng"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","order":{"label_id":"S1.A1.17373471","partner_id":"1234567","status":"5","status_text":"Đã giao hàng","customer_fullname":"Vân Nguyễn","customer_tel":"0911222333","address":"123 nguyễn chí thanh Quận 1, TP Hồ Chí Minh","ship_money":"16500","insurance":"16500","value":"3000000","weight":"300","pick_money":47000,"is_freeship":"1"}}`))
	})
	mux.HandleFunc("POST /services/shipment/cancel/{code}", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r) {
			return
		}
		switch r.PathValue("code") {
		case "PICKED":
			_, _ = w.Write([]byte(`{"success":false,"message":"Đơn đã lấy hàng, không thể hủy đơn."}`))
		case "GONE":
			_, _ = w.Write([]byte(`{"success":false,"message":"Đơn hàng đã đã ở trạng thái hủy"}`)) //nolint:dupword // GHTK's message, word for word.
		default:
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		}
	})
	mux.HandleFunc("GET /services/shipment/fee", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r) {
			return
		}
		q := r.URL.Query()
		if q.Get("weight") != "800" || q.Get("pick_address_id") != "88256" || q.Get("ward") != "Phường Bến Nghé" {
			t.Errorf("fee query %v", q)
		}
		if q.Get("province") == "Nowhere" {
			_, _ = w.Write([]byte(`{"success":true,"message":"","fee":{"name":"area1","fee":0,"insurance_fee":0,"delivery":false}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","fee":{"name":"area1","fee":30400,"insurance_fee":0,"delivery":true}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return carrier.GHTKClient{BaseURL: srv.URL, PartnerToken: "partner", ClientSource: "SNX", HTTP: srv.Client()}
}

var benNghe = carrier.Place{Name: "Vân Nguyễn", Phone: "0911222333", Street: "1 Lê Duẩn", Commune: "Phường Bến Nghé", Province: "TP. Hồ Chí Minh"}

func TestGHTKLinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghtkServer(t)
	if _, err := g.Link(ctx, carrier.Credentials{Email: "a@b.vn", Password: "nope"}); !errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("wrong password: %v", err)
	}
	l, err := g.Link(ctx, carrier.Credentials{Email: "a@b.vn", Password: "right"})
	if err != nil || l.ShopCode != "S19159" || l.Token != "shop-token" {
		t.Fatalf("%+v %v", l, err)
	}
	g.PartnerToken = "revoked"
	if _, err = g.Link(ctx, carrier.Credentials{Email: "a@b.vn", Password: "right"}); err == nil || errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("a refused partner token is ShopNexus's fault, not the seller's: %v", err)
	}
}

func TestGHTKShipsReadsAndCancels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghtkServer(t)
	acc := carrier.Account{Token: "shop-token", ShopCode: "S19159"}
	pts, err := g.Pickups(ctx, acc)
	if err != nil || len(pts) != 1 || pts[0] != (carrier.PickupPoint{ID: "88256", Name: "Store 1", Phone: "0987654321", Address: "Số 105, Hoàng Mai, Hà Nội"}) {
		t.Fatalf("%+v %v", pts, err)
	}
	in := carrier.CreateInput{
		OrderCode: "SN-100001", Recipient: benNghe, Pickup: carrier.Pickup{PointID: "88256"}, Item: "Ghế gỗ",
		COD: 47_000, WeightGrams: 800, Service: "express", Option: "home", Note: "Địa chỉ công ty: giao giờ hành chính",
	}
	c, err := g.Create(ctx, acc, in)
	if err != nil || c.TrackingCode != "S1.A1.2001297581" || c.Fee == nil || *c.Fee != 30_400 {
		t.Fatalf("%+v %v", c, err)
	}
	quiet := in
	quiet.OrderCode, quiet.Note = "SN-NONOTE", ""
	if _, err = g.Create(ctx, acc, quiet); err != nil {
		t.Fatalf("an order without a note: %v", err)
	}
	in.OrderCode = "SN-BAD"
	if _, err = g.Create(ctx, acc, in); !errors.Is(err, carrier.ErrRefused) || !strings.Contains(err.Error(), "không hợp lệ") {
		t.Fatalf("a refused order: %v", err)
	}
	in.OrderCode = "SN-DUP"
	if c, err = g.Create(ctx, acc, in); err != nil || c.TrackingCode != "S1.A1.111" || c.Fee != nil {
		t.Fatalf("a retry of a made order: %+v %v", c, err)
	}
	p, err := g.Lookup(ctx, acc, "S1.A1.17373471")
	if err != nil || p.Step != carrier.StepDelivered || len(p.Events) != 1 || p.Events[0].CarrierStatus != "5" || p.COD != 47_000 || p.Fee == nil || *p.Fee != 16_500 ||
		p.RecipientName != "V** N*****" || p.District != "123 nguyễn chí thanh Quận 1, TP Hồ Chí Minh" {
		t.Fatalf("%+v %v", p, err)
	}
	if late, _ := g.Lookup(ctx, acc, "S1.LATE"); late.Step != carrier.StepDeliveryFailed || late.Status() != carrier.StatusInTransit {
		t.Fatalf("delayed delivery (10): %+v", late)
	}
	if _, err = g.Lookup(ctx, acc, "S1.OTHER"); !errors.Is(err, carrier.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err = g.Lookup(ctx, carrier.Account{Token: "someone-else"}, "S1.A1.17373471"); err == nil || errors.Is(err, carrier.ErrNotFound) {
		t.Fatalf("a refused token: %v", err)
	}
	for code, want := range map[string]error{"S1.A1.2001297581": nil, "GONE": nil, "PICKED": carrier.ErrNotCancellable} {
		if err = g.Cancel(ctx, acc, code); !errors.Is(err, want) {
			t.Errorf("cancel %s: %v", code, err)
		}
	}
}

func TestGHTKQuotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghtkServer(t)
	acc := carrier.Account{Token: "shop-token"}
	in := carrier.QuoteInput{Recipient: benNghe, Pickup: carrier.Pickup{PointID: "88256"}, WeightGrams: 800, Service: "standard", Value: 47_000}
	if fee, err := g.Quote(ctx, acc, in); err != nil || fee != 30_400 {
		t.Fatalf("%d %v", fee, err)
	}
	in.Recipient.Province = "Nowhere"
	if _, err := g.Quote(ctx, acc, in); !errors.Is(err, carrier.ErrAddressUnknown) {
		t.Fatalf("no delivery there: %v", err)
	}
}

func TestGHTKErrorsCarryNoToken(t *testing.T) {
	t.Parallel()
	g := ghtkServer(t)
	_, err := g.Lookup(context.Background(), carrier.Account{Token: "secret-token-123"}, "S1.A1.17373471")
	if err == nil || strings.Contains(err.Error(), "secret-token-123") {
		t.Fatalf("%v", err)
	}
}
