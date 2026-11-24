package carrier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GHTKClient makes and reads shipments at GHTK with each seller's shop token.
// PartnerToken is ShopNexus's B2C token, used only to link accounts;
// ClientSource is ShopNexus's partner code, sent on every call.
type GHTKClient struct {
	BaseURL      string
	PartnerToken string
	ClientSource string
	HTTP         *http.Client
}

var _ Client = GHTKClient{}

// flexInt reads a number GHTK sends as 30400 or as "30400".
type flexInt int64

func (n *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("read %s as a number: %w", b, err)
	}
	*n = flexInt(v)
	return nil
}

type ghtkReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// httpError is an answer other than 200; its status is kept for Link, which
// tells a refused partner token apart.
type httpError struct {
	path   string
	status int
}

func (e httpError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.path, e.status) }

// do calls GHTK with token and decodes the answer into out. Errors name
// the path, never the token.
func (g GHTKClient) do(ctx context.Context, method, path, token string, body, out any) error {
	var rd io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GHTK %s: %w", path, err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, rd)
	if err != nil {
		return fmt.Errorf("build GHTK %s: %w", path, err)
	}
	req.Header.Set("Token", token)
	req.Header.Set("X-Client-Source", g.ClientSource)
	req.Header.Set("Content-Type", "application/json")
	res, err := g.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call GHTK %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GHTK %w", httpError{path: path, status: res.StatusCode})
	}
	if err = json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GHTK %s: %w", path, err)
	}
	return nil
}

// Link trades the seller's email and password for the shop's token
// through GHTK's B2C partner API. The password goes no further.
func (g GHTKClient) Link(ctx context.Context, c Credentials) (Linked, error) {
	var out struct {
		ghtkReply

		Data struct {
			Code  string `json:"code"`
			Token string `json:"token"`
		} `json:"data"`
	}
	err := g.do(ctx, http.MethodPost, "/services/shops/token", g.PartnerToken, map[string]string{"email": c.Email, "password": c.Password}, &out)
	if e, ok := errors.AsType[httpError](err); ok && (e.status == http.StatusUnauthorized || e.status == http.StatusForbidden) {
		return Linked{}, fmt.Errorf("GHTK refused ShopNexus's partner token: %w", err)
	}
	if err != nil {
		return Linked{}, err
	}
	if !out.Success || out.Data.Token == "" {
		return Linked{}, ErrLoginFailed
	}
	return Linked{ShopCode: out.Data.Code, Token: out.Data.Token}, nil
}

func (g GHTKClient) Pickups(ctx context.Context, a Account) ([]PickupPoint, error) {
	var out struct {
		ghtkReply

		Data []struct {
			ID      string `json:"pick_address_id"`
			Address string `json:"address"`
			Tel     string `json:"pick_tel"`
			Name    string `json:"pick_name"`
		} `json:"data"`
	}
	if err := g.do(ctx, http.MethodGet, "/services/shipment/list_pick_add", a.Token, nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("%w: %s", ErrRefused, out.Message)
	}
	pts := make([]PickupPoint, 0, len(out.Data))
	for _, p := range out.Data {
		pts = append(pts, PickupPoint{ID: p.ID, Name: p.Name, Phone: p.Tel, Address: p.Address})
	}
	return pts, nil
}

func (g GHTKClient) Quote(ctx context.Context, a Account, in QuoteInput) (int64, error) {
	q := url.Values{
		"province": {in.Recipient.Province}, "ward": {in.Recipient.Commune}, "address": {in.Recipient.Street},
		"weight": {strconv.Itoa(in.WeightGrams)}, "value": {strconv.FormatInt(in.Value, 10)}, "transport": {ghtkTransport(in.Service)},
	}
	if in.Pickup.PointID != "" {
		q.Set("pick_address_id", in.Pickup.PointID)
	}
	if p := in.Pickup.Place; p != nil {
		q.Set("pick_province", p.Province)
		q.Set("pick_ward", p.Commune)
		q.Set("pick_address", p.Street)
	}
	var out struct {
		ghtkReply

		Fee struct {
			Fee      flexInt `json:"fee"`
			Delivery bool    `json:"delivery"`
		} `json:"fee"`
	}
	if err := g.do(ctx, http.MethodGet, "/services/shipment/fee?"+q.Encode(), a.Token, nil, &out); err != nil {
		return 0, err
	}
	if !out.Success || !out.Fee.Delivery {
		return 0, ErrAddressUnknown
	}
	return int64(out.Fee.Fee), nil
}

// Create makes the order with ShopNexus's order code as GHTK's partner id,
// so a retry finds the order the first try made.
func (g GHTKClient) Create(ctx context.Context, a Account, in CreateInput) (Created, error) {
	order := map[string]any{
		"id": in.OrderCode, "name": in.Recipient.Name, "tel": in.Recipient.Phone, "address": in.Recipient.Street,
		"province": in.Recipient.Province, "ward": in.Recipient.Commune, "hamlet": "Khác",
		"pick_money": in.COD, "value": in.COD, "is_freeship": 0, "transport": ghtkTransport(in.Service),
		"pick_option": map[string]string{"home": "cod", "post_office": "post"}[in.Option],
	}
	if in.Note != "" {
		order["note"] = in.Note
	}
	if in.Pickup.PointID != "" {
		order["pick_address_id"] = in.Pickup.PointID
	}
	if p := in.Pickup.Place; p != nil {
		order["pick_name"], order["pick_tel"], order["pick_address"] = p.Name, p.Phone, p.Street
		order["pick_province"], order["pick_ward"] = p.Province, p.Commune
	}
	body := map[string]any{
		"products": []map[string]any{{"name": in.Item, "weight": float64(in.WeightGrams) / 1000, "quantity": 1, "price": in.COD}},
		"order":    order,
	}
	var out struct {
		ghtkReply

		Order struct {
			Label string  `json:"label"`
			Fee   flexInt `json:"fee"`
		} `json:"order"`
		Error struct {
			Code  string `json:"code"`
			Label string `json:"ghtk_label"`
		} `json:"error"`
	}
	if err := g.do(ctx, http.MethodPost, "/services/shipment/order", a.Token, body, &out); err != nil {
		return Created{}, err
	}
	switch {
	case out.Success:
		return Created{TrackingCode: out.Order.Label, Fee: int64(out.Order.Fee)}, nil
	case out.Error.Code == "ORDER_ID_EXIST" && out.Error.Label != "":
		// A retry: the first try made it. Its fee comes with the next read.
		return Created{TrackingCode: out.Error.Label}, nil
	}
	return Created{}, fmt.Errorf("%w: %s", ErrRefused, out.Message)
}

func (g GHTKClient) Lookup(ctx context.Context, a Account, code string) (Parcel, error) {
	var out struct {
		ghtkReply

		Order struct {
			Status   flexInt `json:"status"`
			Name     string  `json:"customer_fullname"`
			Address  string  `json:"address"`
			ShipFee  flexInt `json:"ship_money"`
			PickCash flexInt `json:"pick_money"`
			Modified string  `json:"modified"`
		} `json:"order"`
	}
	if err := g.do(ctx, http.MethodGet, "/services/shipment/v2/"+url.PathEscape(code), a.Token, nil, &out); err != nil {
		return Parcel{}, err
	}
	if !out.Success {
		return Parcel{}, ErrNotFound
	}
	fee := int64(out.Order.ShipFee)
	p := Parcel{RecipientName: mask(out.Order.Name), District: out.Order.Address, COD: int64(out.Order.PickCash), Fee: &fee}
	// GHTK tells only the status it is at now.
	if st, ok := GHTKStep(int(out.Order.Status)); ok {
		p.Step = st
		p.Events = []Event{{
			At: readTime(out.Order.Modified, time.Now()), Kind: KindStep, Step: st, CarrierStatus: strconv.Itoa(int(out.Order.Status)),
		}}
	}
	return p, nil
}

// Cancel calls the order off by its label. GHTK's samples use GET where
// its text says POST; a 405 to POST retries with GET.
func (g GHTKClient) Cancel(ctx context.Context, a Account, code string) error {
	path := "/services/shipment/cancel/" + url.PathEscape(code)
	var out ghtkReply
	err := g.do(ctx, http.MethodPost, path, a.Token, nil, &out)
	if e, ok := errors.AsType[httpError](err); ok && e.status == http.StatusMethodNotAllowed {
		err = g.do(ctx, http.MethodGet, path, a.Token, nil, &out)
	}
	if err != nil {
		return err
	}
	switch {
	case out.Success, strings.Contains(out.Message, "trạng thái hủy"):
		return nil
	case strings.Contains(out.Message, "không thể hủy"):
		return ErrNotCancellable
	}
	return fmt.Errorf("%w: %s", ErrRefused, out.Message)
}

func ghtkTransport(service string) string {
	if service == "express" {
		return "fly"
	}
	return "road"
}
