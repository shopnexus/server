package carrier_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/provider/carrier"
)

// ghnServer plays GHN: answers carry code and code_message, and an error
// comes with an HTTP status other than 200 as well.
func ghnServer(t *testing.T) carrier.GHNClient {
	t.Helper()
	fail := func(w http.ResponseWriter, status int, codeMessage string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"code":` + map[int]string{400: "400", 401: "401"}[status] + `,"message":"refused","code_message":"` + codeMessage + `","data":null}`))
	}
	shop := func(w http.ResponseWriter, r *http.Request, needShop bool) bool {
		if r.Header.Get("Token") != "ghn-token" {
			fail(w, http.StatusUnauthorized, "")
			return false
		}
		if needShop && r.Header.Get("Shopid") != "885" {
			fail(w, http.StatusBadRequest, "CLIENT_NOT_OWNER_OF_SHOP")
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /shiip/public-api/v2/shop/all", func(w http.ResponseWriter, r *http.Request) {
		if shop(w, r, false) {
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"last_offset":885,"shops":[{"_id":885,"name":"Kho Bình Thạnh","phone":"0987654321","address":"12 Nguyễn Gia Trí, Bình Thạnh","ward_code":"20308","district_id":1462,"status":1}]}}`))
		}
	})
	mux.HandleFunc("POST /shiip/public-api/v2/shipping-order/detail", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r, false) {
			return
		}
		var in struct {
			OrderCode string `json:"order_code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.OrderCode == "ODD" {
			fail(w, http.StatusBadRequest, "USER_ERR_COMMON")
			return
		}
		if in.OrderCode != "5ENLKKHD" {
			fail(w, http.StatusBadRequest, "ORDER_NOT_FOUND")
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"order_code":"5ENLKKHD","client_order_code":"SN-100001","status":"delivered","to_name":"Vân Nguyễn","to_phone":"0911222333","to_address":"1 Lê Duẩn, Phường Bến Nghé, TP. Hồ Chí Minh","cod_amount":47000,"weight":800}}`))
	})
	order := func(w http.ResponseWriter, r *http.Request, preview bool) {
		if !shop(w, r, true) {
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !preview && in["client_order_code"] == "SN-DUP" {
			fail(w, http.StatusBadRequest, "USER_ERR_COMMON")
			return
		}
		if in["to_phone"] == "bad" {
			fail(w, http.StatusBadRequest, "PHONE_INVALID")
			return
		}
		if in["to_province_name"] == "Nowhere" {
			fail(w, http.StatusBadRequest, "ROUTE_NOT_FOUND_SERVICE")
			return
		}
		if fmt.Sprint(in["is_new_to_address"]) != "true" || in["to_ward_name"] != "Phường Bến Nghé" || in["to_province_name"] != "TP. Hồ Chí Minh" ||
			in["to_name"] != "Vân Nguyễn" || in["weight"] != float64(800) || in["service_type_id"] != float64(2) ||
			in["payment_type_id"] != float64(2) || in["required_note"] != "CHOXEMHANGKHONGTHU" {
			t.Errorf("order body %+v", in)
		}
		if !preview && (in["client_order_code"] != "SN-100001" || in["cod_amount"] != float64(47_000) ||
			in["note"] != "Địa chỉ công ty: giao giờ hành chính") {
			t.Errorf("order body %+v", in)
		}
		if _, has := in["note"]; preview && has {
			t.Errorf("a preview with a note: %+v", in)
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"order_code":"5ENLKKHD","sort_code":"19-00-00","fee":{"main_service":33000},"total_fee":33000,"expected_delivery_time":"2026-11-15T16:59:59Z"}}`))
	}
	mux.HandleFunc("POST /shiip/public-api/v2/shipping-order/create", func(w http.ResponseWriter, r *http.Request) { order(w, r, false) })
	mux.HandleFunc("POST /shiip/public-api/v2/shipping-order/preview", func(w http.ResponseWriter, r *http.Request) { order(w, r, true) })
	mux.HandleFunc("POST /shiip/public-api/v2/shipping-order/detail-by-client-code", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r, false) {
			return
		}
		var in struct {
			Code string `json:"client_order_code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Code != "SN-DUP" {
			fail(w, http.StatusBadRequest, "ORDER_NOT_FOUND")
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"order_code":"5DUPLKHD","client_order_code":"SN-DUP","status":"ready_to_pick"}}`))
	})
	mux.HandleFunc("POST /shiip/public-api/v2/switch-status/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !shop(w, r, true) {
			return
		}
		var in struct {
			OrderCodes []string `json:"order_codes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch in.OrderCodes[0] {
		case "UNKNOWN":
			fail(w, http.StatusBadRequest, "ORDER_NOT_FOUND")
		case "PICKED":
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":[{"order_code":"PICKED","result":false,"message":"Đơn hàng không thể hủy"}]}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":[{"order_code":"5ENLKKHD","result":true,"message":"OK"}]}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return carrier.GHNClient{BaseURL: srv.URL + "/shiip/public-api", HTTP: srv.Client()}
}

func TestGHNLinksWithAShopOfTheToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghnServer(t)
	l, err := g.Link(ctx, carrier.Credentials{Token: "ghn-token", ShopID: "885"})
	if err != nil || l != (carrier.Linked{ShopCode: "885", Token: "ghn-token"}) {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err = g.Link(ctx, carrier.Credentials{Token: "ghn-token", ShopID: "999"}); !errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("another shop: %v", err)
	}
	if _, err = g.Link(ctx, carrier.Credentials{Token: "nope", ShopID: "885"}); !errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("a wrong token: %v", err)
	}
	pts, err := g.Pickups(ctx, carrier.Account{Token: "ghn-token", ShopCode: "885"})
	if err != nil || len(pts) != 1 || pts[0] != (carrier.PickupPoint{ID: "885", Name: "Kho Bình Thạnh", Phone: "0987654321", Address: "12 Nguyễn Gia Trí, Bình Thạnh"}) {
		t.Fatalf("%+v %v", pts, err)
	}
}

func TestGHNShipsReadsAndCancels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghnServer(t)
	acc := carrier.Account{Token: "ghn-token", ShopCode: "885"}
	in := carrier.CreateInput{
		OrderCode: "SN-100001", Recipient: benNghe, Pickup: carrier.Pickup{PointID: "885"}, Item: "Ghế gỗ",
		COD: 47_000, WeightGrams: 800, Service: "standard", Option: "home", Note: "Địa chỉ công ty: giao giờ hành chính",
	}
	c, err := g.Create(ctx, acc, in)
	if err != nil || c != (carrier.Created{TrackingCode: "5ENLKKHD", Fee: 33_000}) {
		t.Fatalf("%+v %v", c, err)
	}
	bad := in
	bad.Recipient.Phone = "bad"
	if _, err = g.Create(ctx, acc, bad); !errors.Is(err, carrier.ErrRefused) {
		t.Fatalf("a refused order: %v", err)
	}
	// GHN refuses a client_order_code it has: the first try made the order.
	dup := in
	dup.OrderCode = "SN-DUP"
	if c, err = g.Create(ctx, acc, dup); err != nil || c.TrackingCode != "5DUPLKHD" {
		t.Fatalf("a retry of a made order: %+v %v", c, err)
	}
	in.Recipient.Province = "Nowhere"
	if _, err = g.Create(ctx, acc, in); !errors.Is(err, carrier.ErrAddressUnknown) {
		t.Fatalf("no route: %v", err)
	}
	p, err := g.Lookup(ctx, acc, "5ENLKKHD")
	if err != nil || p.Status != carrier.StatusDelivered || p.COD != 47_000 || p.RecipientName != "V** N*****" ||
		p.District != "1 Lê Duẩn, Phường Bến Nghé, TP. Hồ Chí Minh" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = g.Lookup(ctx, acc, "ODD"); !errors.Is(err, carrier.ErrNotFound) {
		t.Fatalf("a 400 of another code_message: %v", err)
	}
	if _, err = g.Lookup(ctx, acc, "OTHER"); !errors.Is(err, carrier.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	for code, want := range map[string]error{"5ENLKKHD": nil, "PICKED": carrier.ErrNotCancellable, "UNKNOWN": carrier.ErrNotFound} {
		if err = g.Cancel(ctx, acc, code); !errors.Is(err, want) {
			t.Errorf("cancel %s: %v", code, err)
		}
	}
}

func TestGHNQuotesWithAPreview(t *testing.T) {
	t.Parallel()
	g := ghnServer(t)
	fee, err := g.Quote(context.Background(), carrier.Account{Token: "ghn-token", ShopCode: "885"},
		carrier.QuoteInput{Recipient: benNghe, WeightGrams: 800, Service: "express", Value: 47_000})
	if err != nil || fee != 33_000 {
		t.Fatalf("%d %v", fee, err)
	}
}
