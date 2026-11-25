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
	// The seller's token owns shop 885; ShopNexus's is staff of 241109.
	shop := func(w http.ResponseWriter, r *http.Request, needShop bool) bool {
		ownShop := map[string]string{"ghn-token": "885", "partner-token": "241109"}[r.Header.Get("Token")]
		if ownShop == "" {
			fail(w, http.StatusUnauthorized, "")
			return false
		}
		if needShop && r.Header.Get("Shopid") != ownShop {
			fail(w, http.StatusBadRequest, "CLIENT_NOT_OWNER_OF_SHOP")
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /shiip/public-api/v2/shop/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Token") == "partner-token" {
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"shops":[{"_id":241000,"phone":"0356875408"},{"_id":241109,"phone":"0356875408"},{"_id":241200,"phone":"0911000000"}]}}`))
			return
		}
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
		_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"order_code":"5ENLKKHD","client_order_code":"SN-100001","status":"delivered","to_name":"Vân Nguyễn","to_phone":"0911222333","to_address":"1 Lê Duẩn, Phường Bến Nghé, TP. Hồ Chí Minh","cod_amount":47000,"weight":800,"log":[{"status":"picked","updated_date":"2026-11-24T02:00:00Z"},{"status":"delivered","updated_date":"2026-11-25T03:30:00Z"}]}}`))
	})
	affiliate := func(w http.ResponseWriter, r *http.Request) (phone, otp string, shop int64, ok bool) {
		if r.Header.Get("Token") != "partner-token" {
			fail(w, http.StatusUnauthorized, "")
			return "", "", 0, false
		}
		var in struct {
			Phone  string `json:"phone"`
			OTP    string `json:"otp"`
			ShopID int64  `json:"shop_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Phone != "0356875408" && in.Phone != "0356870001" {
			fail(w, http.StatusBadRequest, "USER_ERR_COMMON")
			return "", "", 0, false
		}
		if in.OTP != "" && in.OTP != "111111" {
			fail(w, http.StatusBadRequest, "OTP_NOT_VALID")
			return "", "", 0, false
		}
		return in.Phone, in.OTP, in.ShopID, true
	}
	mux.HandleFunc("POST /shiip/public-api/v2/shop/affiliateOTP", func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := affiliate(w, r); ok {
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"TTL":600}}`))
		}
	})
	// ShopNexus is staff of 241000 and 241109, both 0356875408's.
	mux.HandleFunc("POST /shiip/public-api/v2/shop/affiliateCreateWithShop", func(w http.ResponseWriter, r *http.Request) {
		if _, _, shop, ok := affiliate(w, r); ok {
			if shop != 241109 {
				t.Errorf("joined %d, not the newest shop", shop)
			}
			fail(w, http.StatusBadRequest, "CLIENT_HAVE_EXISTED")
		}
	})
	mux.HandleFunc("POST /shiip/public-api/v2/shop/affiliateCreate", func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := affiliate(w, r); ok {
			_, _ = w.Write([]byte(`{"code":200,"message":"Success","data":{"client_id":2551400,"shop_id":241300}}`))
		}
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
		// A place is the sender and the return address, in the two-level scheme.
		if in["from_name"] != nil {
			for _, side := range []string{"from", "return"} {
				if in[side+"_name"] != "Tú" || in[side+"_phone"] != "0909000111" || in[side+"_address"] != "5 Phan Xích Long" ||
					in[side+"_ward_name"] != "Phường Cầu Kiệu" || in[side+"_province_name"] != "TP. Hồ Chí Minh" ||
					in[side+"_district_name"] != "" || fmt.Sprint(in["is_new_"+side+"_address"]) != "true" {
					t.Errorf("%s of %+v", side, in)
				}
			}
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
	return carrier.GHNClient{BaseURL: srv.URL + "/shiip/public-api", PartnerToken: "partner-token", HTTP: srv.Client()}
}

func TestGHNLinksByATextedCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := ghnServer(t)
	if err := g.SendOTP(ctx, "+84356875408"); err != nil {
		t.Fatal(err)
	}
	if err := g.SendOTP(ctx, "+84911000000"); !errors.Is(err, carrier.ErrPhoneUnknown) {
		t.Fatalf("a phone without a GHN account: %v", err)
	}
	// A phone with no shop ShopNexus joined gets a new one.
	l, err := g.Link(ctx, carrier.Credentials{Phone: "+84356870001", OTP: "111111"})
	if err != nil || l != (carrier.Linked{ShopCode: "241300"}) {
		t.Fatalf("%+v %v", l, err)
	}
	// Joined before: the newest shop of that phone's, no second one.
	if l, err = g.Link(ctx, carrier.Credentials{Phone: "+84356875408", OTP: "111111"}); err != nil || l != (carrier.Linked{ShopCode: "241109"}) {
		t.Fatalf("joined before: %+v %v", l, err)
	}
	for _, phone := range []string{"+84356875408", "+84356870001"} {
		if _, err = g.Link(ctx, carrier.Credentials{Phone: phone, OTP: "999999"}); !errors.Is(err, carrier.ErrOTPInvalid) {
			t.Fatalf("a wrong code for %s: %v", phone, err)
		}
	}
	// The joined shop ships on ShopNexus's token, from the pickup place.
	acc := carrier.Account{ShopCode: "241109"}
	var pts []carrier.PickupPoint
	if pts, err = g.Pickups(ctx, acc); err != nil || len(pts) != 0 {
		t.Fatalf("pickups %+v %v", pts, err)
	}
	from := carrier.Place{Name: "Tú", Phone: "0909000111", Street: "5 Phan Xích Long", Commune: "Phường Cầu Kiệu", Province: "TP. Hồ Chí Minh"}
	c, err := g.Create(ctx, acc, carrier.CreateInput{
		OrderCode: "SN-100001", Recipient: benNghe, Pickup: carrier.Pickup{Place: &from}, Item: "Ghế gỗ",
		COD: 47_000, WeightGrams: 800, Service: "standard", Option: "home", Note: "Địa chỉ công ty: giao giờ hành chính",
	})
	if err != nil || c.TrackingCode != "5ENLKKHD" {
		t.Fatalf("%+v %v", c, err)
	}
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
	if err != nil || c.TrackingCode != "5ENLKKHD" || c.Fee == nil || *c.Fee != 33_000 || c.ShopCode != "" {
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
	// Its detail tells no fee, so the shipment has none yet.
	if c, err = g.Create(ctx, acc, dup); err != nil || c.TrackingCode != "5DUPLKHD" || c.Fee != nil {
		t.Fatalf("a retry of a made order: %+v %v", c, err)
	}
	in.Recipient.Province = "Nowhere"
	if _, err = g.Create(ctx, acc, in); !errors.Is(err, carrier.ErrAddressUnknown) {
		t.Fatalf("no route: %v", err)
	}
	p, err := g.Lookup(ctx, acc, "5ENLKKHD")
	if err != nil || p.Step != carrier.StepDelivered || len(p.Events) != 2 || p.Events[1].CarrierStatus != "delivered" || p.COD != 47_000 || p.RecipientName != "V** N*****" ||
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
