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
	"strings"
	"time"
)

// GHNClient makes and reads shipments at GHN with each seller's token and
// ShopId. BaseURL ends in /shiip/public-api. Addresses go as names in
// GHN's two-level scheme (commune and province), so no district is needed.
// PartnerToken is ShopNexus's own GHN token: it links a seller's shop by a
// texted code and calls the shops ShopNexus joined that way.
type GHNClient struct {
	BaseURL      string
	PartnerToken string
	HTTP         *http.Client
}

var _ Texter = GHNClient{}

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
	token := a.Token
	if token == "" {
		token = g.PartnerToken
	}
	req.Header.Set("Token", token)
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

// refusal is err as ErrRefused when GHN answered it with a 4xx other than
// 401 (a token GHN does not take), and err otherwise.
func refusal(err error) error {
	if e, ok := errors.AsType[ghnError](err); ok && e.status >= 400 && e.status < 500 && e.status != http.StatusUnauthorized {
		return RefusedError{Reason: e.codeMessage}
	}
	return err
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

// Link checks that the ShopId is one of the token's shops, or with a code
// joins the seller's GHN account through ShopNexus's.
func (g GHNClient) Link(ctx context.Context, c Credentials) (Linked, error) {
	if c.OTP != "" {
		return g.join(ctx, c.Phone, c.OTP)
	}
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

// ghnPhone is phone as GHN takes it, 0 and nine digits.
func ghnPhone(phone string) string {
	if rest, ok := strings.CutPrefix(phone, "+84"); ok {
		return "0" + rest
	}
	return phone
}

// SendOTP has GHN text the owner of phone the code that join takes.
func (g GHNClient) SendOTP(ctx context.Context, phone string) error {
	var out json.RawMessage
	err := g.do(ctx, http.MethodPost, "/v2/shop/affiliateOTP", Account{}, false,
		map[string]string{"phone": ghnPhone(phone), "language": "vi"}, &out)
	if codeMessage(err) == "USER_ERR_COMMON" {
		return ErrPhoneUnknown
	}
	return err
}

// join links the seller's newest shop that ShopNexus is staff of, or
// makes one. A new shop's address is the seller's GHN default, else
// ShopNexus's; orders send their own. GHN checks the code first.
func (g GHNClient) join(ctx context.Context, phone, otp string) (Linked, error) {
	phone = ghnPhone(phone)
	shops, err := g.shops(ctx, "")
	if err != nil {
		return Linked{}, err
	}
	var id int64
	for _, s := range shops {
		if s.Phone == phone && s.ID > id {
			id = s.ID
		}
	}
	if id != 0 {
		var none json.RawMessage
		err = g.do(ctx, http.MethodPost, "/v2/shop/affiliateCreateWithShop", Account{}, false,
			map[string]any{"phone": phone, "otp": otp, "shop_id": id, "language": "vi"}, &none)
		switch codeMessage(err) {
		case "OTP_NOT_VALID":
			return Linked{}, ErrOTPInvalid
		case "CLIENT_HAVE_EXISTED":
			return Linked{ShopCode: strconv.FormatInt(id, 10)}, nil
		case "":
			if err != nil {
				return Linked{}, err
			}
			return Linked{ShopCode: strconv.FormatInt(id, 10)}, nil
		default:
			// Gone or no longer the phone's: a new shop below.
		}
	}
	var out struct {
		ShopID int64 `json:"shop_id"`
	}
	err = g.do(ctx, http.MethodPost, "/v2/shop/affiliateCreate", Account{}, false,
		map[string]string{"phone": phone, "otp": otp, "language": "vi"}, &out)
	switch codeMessage(err) {
	case "":
		if err != nil {
			return Linked{}, err
		}
		return Linked{ShopCode: strconv.FormatInt(out.ShopID, 10)}, nil
	case "OTP_NOT_VALID":
		return Linked{}, ErrOTPInvalid
	case "USER_ERR_COMMON":
		return Linked{}, ErrPhoneUnknown
	default:
		return Linked{}, refusal(err)
	}
}

// Pickups is the linked shop: at GHN a shop is a pickup address. A shop
// ShopNexus joined has none to offer, its address being a fallback.
func (g GHNClient) Pickups(ctx context.Context, a Account) ([]PickupPoint, error) {
	if a.Token == "" {
		return []PickupPoint{}, nil
	}
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
func ghnOrderBody(code string, to Place, from Pickup, item string, cod int64, weight int, note string) map[string]any {
	body := map[string]any{
		"payment_type_id": 2, "required_note": "CHOXEMHANGKHONGTHU", "service_type_id": 2,
		"to_name": to.Name, "to_phone": ghnPhone(to.Phone), "to_address": to.Street,
		"to_ward_name": to.Commune, "to_province_name": to.Province, "to_district_name": "", "is_new_to_address": true,
		"cod_amount": cod, "weight": weight, "content": item,
		"items": []map[string]any{{"name": item, "quantity": 1, "weight": weight}},
	}
	if code != "" {
		body["client_order_code"] = code
	}
	if note != "" {
		body["note"] = note
	}
	// A place is the sender and the return address, both two-level.
	if p := from.Place; p != nil {
		for _, side := range []string{"from", "return"} {
			body[side+"_name"], body[side+"_phone"], body[side+"_address"] = p.Name, ghnPhone(p.Phone), p.Street
			body[side+"_ward_name"], body[side+"_province_name"], body[side+"_district_name"] = p.Commune, p.Province, ""
			body["is_new_"+side+"_address"] = true
		}
	}
	return body
}

// ghnOrder is GHN's answer to a create or a preview; TotalFee is nil
// when GHN left it out.
type ghnOrder struct {
	OrderCode string `json:"order_code"`
	TotalFee  *int64 `json:"total_fee"`
}

// Quote prices the shipment with GHN's preview, which takes the create
// body and makes nothing: GHN's fee API still wants district ids.
func (g GHNClient) Quote(ctx context.Context, a Account, in QuoteInput) (int64, error) {
	var out ghnOrder
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/preview", a, true,
		ghnOrderBody("", in.Recipient, in.Pickup, "Hàng", 0, in.WeightGrams, ""), &out)
	if codeMessage(err) == "ROUTE_NOT_FOUND_SERVICE" {
		return 0, ErrAddressUnknown
	}
	if err != nil {
		return 0, refusal(err)
	}
	if out.TotalFee == nil {
		return 0, nil
	}
	return *out.TotalFee, nil
}

// Create makes the order with ShopNexus's order code as client_order_code.
// A refused create is read back by that code first: a retry whose first
// try made the order finds it there.
func (g GHNClient) Create(ctx context.Context, a Account, in CreateInput) (Created, error) {
	var out ghnOrder
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/create", a, true,
		ghnOrderBody(in.OrderCode, in.Recipient, in.Pickup, in.Item, in.COD, in.WeightGrams, in.Note), &out)
	if codeMessage(err) == "ROUTE_NOT_FOUND_SERVICE" {
		return Created{}, ErrAddressUnknown
	}
	if err = refusal(err); errors.Is(err, ErrRefused) {
		var made ghnOrder
		if g.do(ctx, http.MethodPost, "/v2/shipping-order/detail-by-client-code", a, false,
			map[string]string{"client_order_code": in.OrderCode}, &made) == nil && made.OrderCode != "" {
			return Created{TrackingCode: made.OrderCode, Fee: made.TotalFee}, nil
		}
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
		Log     []struct {
			Status string `json:"status"`
			At     string `json:"updated_date"`
		} `json:"log"`
	}
	err := g.do(ctx, http.MethodPost, "/v2/shipping-order/detail", a, false, map[string]string{"order_code": code}, &out)
	// The code is the only thing detail takes, so any 400 is a code the
	// account does not have.
	if e, ok := errors.AsType[ghnError](err); ok && e.status == http.StatusBadRequest {
		return Parcel{}, ErrNotFound
	}
	if err != nil {
		return Parcel{}, err
	}
	p := Parcel{RecipientName: mask(out.ToName), District: out.Address, COD: out.COD}
	p.Step = ghnSteps[out.Status]
	now := time.Now()
	for _, l := range out.Log {
		if st, ok := ghnSteps[l.Status]; ok {
			p.Events = append(p.Events, Event{At: readTime(l.At, now), Kind: KindStep, Step: st, CarrierStatus: l.Status})
		}
	}
	return p, nil
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
		return refusal(err)
	}
	if len(out) == 0 || !out[0].Result {
		return ErrNotCancellable
	}
	return nil
}

// ghnSteps maps GHN's order status; exception maps to none.
var ghnSteps = map[string]Step{
	"ready_to_pick": StepReadyToPick, "picking": StepPicking, "money_collect_picking": StepPicking,
	"picked": StepPicked, "storing": StepAtHub, "sorting": StepAtHub, "transporting": StepTransporting,
	"delivering": StepOutForDelivery, "money_collect_delivering": StepOutForDelivery,
	"delivery_fail": StepDeliveryFailed, "delivered": StepDelivered,
	"waiting_to_return": StepReturning, "return": StepReturning, "return_transporting": StepReturning,
	"return_sorting": StepReturning, "returning": StepReturning, "return_fail": StepReturning,
	"returned": StepReturned, "cancel": StepCancelled, "lost": StepLost, "damage": StepLost,
}

// Report reads GHN's JSON by OrderCode and Type: a create or a
// switch_status tells a step, update_weight, update_fee and update_cod an
// amount. A paid-out COD comes with CODTransferDate on any of them.
func (GHNClient) Report(contentType string, body []byte, now time.Time) (string, []Event, error) {
	f, err := reportFields(contentType, body)
	if err != nil {
		return "", nil, err
	}
	if f["OrderCode"] == "" {
		return "", nil, errNoCode
	}
	at := readTime(f["Time"], now)
	var evs []Event
	switch strings.ToLower(f["Type"]) {
	case "", "create", "switch_status":
		if st, ok := ghnSteps[f["Status"]]; ok {
			evs = append(evs, Event{
				At: at, Kind: KindStep, Step: st, CarrierStatus: f["Status"],
				Reason: f["Reason"], ReasonCode: f["ReasonCode"], Warehouse: f["Warehouse"],
				ShipperName: f["ShipperName"], ShipperPhone: f["ShipperPhone"], ProofURL: f["PodURL"],
			})
		}
	case "update_weight":
		evs = appendAmount(evs, at, KindWeight, f["Weight"])
	case "update_fee":
		evs = appendAmount(evs, at, KindFee, f["TotalFee"])
	case "update_cod":
		evs = appendAmount(evs, at, KindCOD, f["CODAmount"])
	default: // another Type tells nothing kept
	}
	if paid := f["CODTransferDate"]; paid != "" {
		evs = appendAmount(evs, readTime(paid, now), KindCODTransfer, f["CODAmount"])
	}
	return f["OrderCode"], evs, nil
}
