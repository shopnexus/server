package carrier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// GHNClient makes and reads shipments at GHN with each seller's token and
// ShopId. BaseURL ends in /shiip/public-api. Addresses go as names in
// GHN's two-level scheme (commune and province), so no district is needed.
type GHNClient struct {
	BaseURL string
	HTTP    *http.Client
}

var _ Client = GHNClient{}

// ghnError is GHN's refusal; codeMessage names it, as GHN's docs advise
// branching on it rather than on the message.
type ghnError struct {
	path        string
	status      int
	codeMessage string
}

func (e ghnError) Error() string {
	return fmt.Sprintf("GHN %s: HTTP %d %s", e.path, e.status, e.codeMessage)
}

// do calls GHN with the account's token, and its ShopId when shop is
// set, and decodes data into out. Errors name the path, never the token.
func (g GHNClient) do(ctx context.Context, method, path string, a Account, shop bool, body, out any) error {
	var rd io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GHN %s: %w", path, err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, rd)
	if err != nil {
		return fmt.Errorf("build GHN %s: %w", path, err)
	}
	req.Header.Set("Token", a.Token)
	if shop {
		req.Header.Set("Shopid", a.ShopCode)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := g.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call GHN %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	var reply struct {
		Code        int             `json:"code"`
		CodeMessage string          `json:"code_message"`
		Data        json.RawMessage `json:"data"`
	}
	// A refusal may come without a JSON body, an HTTP 401 for one.
	_ = json.NewDecoder(res.Body).Decode(&reply)
	if res.StatusCode != http.StatusOK || reply.Code != http.StatusOK {
		return ghnError{path: path, status: res.StatusCode, codeMessage: reply.CodeMessage}
	}
	if err = json.Unmarshal(reply.Data, out); err != nil {
		return fmt.Errorf("decode GHN %s: %w", path, err)
	}
	return nil
}

// codeMessage is the code_message of a GHN refusal, or "".
func codeMessage(err error) string {
	if e, ok := errors.AsType[ghnError](err); ok {
		return e.codeMessage
	}
	return ""
}

type ghnShop struct {
	ID      int64  `json:"_id"` //nolint:tagliatelle // GHN names it so.
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

// shops lists the shops of the token's GHN account.
func (g GHNClient) shops(ctx context.Context, token string) ([]ghnShop, error) {
	var out struct {
		Shops []ghnShop `json:"shops"`
	}
	if err := g.do(ctx, http.MethodGet, "/v2/shop/all", Account{Token: token}, false, nil, &out); err != nil {
		return nil, err
	}
	return out.Shops, nil
}

// Link checks that the ShopId is one of the token's shops.
func (g GHNClient) Link(ctx context.Context, c Credentials) (Linked, error) {
	shops, err := g.shops(ctx, c.Token)
	if e, ok := errors.AsType[ghnError](err); ok && e.status == http.StatusUnauthorized {
		return Linked{}, ErrLoginFailed
	}
	if err != nil {
		return Linked{}, err
	}
	for _, s := range shops {
		if strconv.FormatInt(s.ID, 10) == c.ShopID {
			return Linked{ShopCode: c.ShopID, Token: c.Token}, nil
		}
	}
	return Linked{}, ErrLoginFailed
}

// Pickups is the linked shop: at GHN a shop is a pickup address.
func (g GHNClient) Pickups(ctx context.Context, a Account) ([]PickupPoint, error) {
	shops, err := g.shops(ctx, a.Token)
	if err != nil {
		return nil, err
	}
	for _, s := range shops {
		if strconv.FormatInt(s.ID, 10) == a.ShopCode {
			return []PickupPoint{{ID: a.ShopCode, Name: s.Name, Phone: s.Phone, Address: s.Address}}, nil
		}
	}
	return []PickupPoint{}, nil
}

// orderBody is GHN's create and preview body. A pickup point is the
// linked shop, whose address GHN has; a place goes as from_ fields.
func ghnOrderBody(code string, to Place, from Pickup, item string, cod int64, weight int) map[string]any {
	body := map[string]any{
		"payment_type_id": 2, "required_note": "CHOXEMHANGKHONGTHU", "service_type_id": 2,
		"to_name": to.Name, "to_phone": to.Phone, "to_address": to.Street,
		"to_ward_name": to.Commune, "to_province_name": to.Province, "to_district_name": "", "is_new_to_address": true,
		"cod_amount": cod, "weight": weight, "content": item,
		"items": []map[string]any{{"name": item, "quantity": 1, "weight": weight}},
	}
	if code != "" {
		body["client_order_code"] = code
	}
	if p := from.Place; p != nil {
		body["from_name"], body["from_phone"], body["from_address"] = p.Name, p.Phone, p.Street
		body["from_ward_name"], body["from_province_name"] = p.Commune, p.Province
	}
	return body
}

type ghnOrder struct {
	OrderCode string `json:"order_code"`
	TotalFee  int64  `json:"total_fee"`
}

// Quote prices the shipment with GHN's preview, which takes the create
// body and makes nothing: GHN's fee API still wants district ids.
func (g GHNClient) Quote(ctx context.Context, a Account, in QuoteInput) (int64, error) {
	var out ghnOrder
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/preview", a, true,
		ghnOrderBody("", in.Recipient, in.Pickup, "Hàng", 0, in.WeightGrams), &out)
	if codeMessage(err) == "ROUTE_NOT_FOUND_SERVICE" {
		return 0, ErrAddressUnknown
	}
	if err != nil {
		return 0, err
	}
	return out.TotalFee, nil
}

// Create makes the order with ShopNexus's order code as client_order_code;
// GHN answers a retry with the order the first try made.
func (g GHNClient) Create(ctx context.Context, a Account, in CreateInput) (Created, error) {
	var out ghnOrder
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/create", a, true,
		ghnOrderBody(in.OrderCode, in.Recipient, in.Pickup, in.Item, in.COD, in.WeightGrams), &out)
	if codeMessage(err) == "ROUTE_NOT_FOUND_SERVICE" {
		return Created{}, ErrAddressUnknown
	}
	if err != nil {
		return Created{}, err
	}
	return Created{TrackingCode: out.OrderCode, Fee: out.TotalFee}, nil
}

func (g GHNClient) Lookup(ctx context.Context, a Account, code string) (Parcel, error) {
	var out struct {
		Status  string `json:"status"`
		ToName  string `json:"to_name"`
		Address string `json:"to_address"`
		COD     int64  `json:"cod_amount"`
	}
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/detail", a, false, map[string]string{"order_code": code}, &out)
	if codeMessage(err) == "ORDER_NOT_FOUND" {
		return Parcel{}, ErrNotFound
	}
	if err != nil {
		return Parcel{}, err
	}
	status, moved := GHNStatus(out.Status)
	if !moved {
		status = StatusPendingPickup
	}
	return Parcel{RecipientName: mask(out.ToName), District: out.Address, COD: out.COD, Status: status}, nil
}

func (g GHNClient) Cancel(ctx context.Context, a Account, code string) error {
	var out []struct {
		Result  bool   `json:"result"`
		Message string `json:"message"`
	}
	err := g.do(ctx, http.MethodPost, "/v2/switch-status/cancel", a, true, map[string][]string{"order_codes": {code}}, &out)
	if codeMessage(err) == "ORDER_NOT_FOUND" {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if len(out) == 0 || !out[0].Result {
		return ErrNotCancellable
	}
	return nil
}
