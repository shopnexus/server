// Package chotot reads a seller's profile and listings from Chợ Tốt's
// public gateway and their photos from Chợ Tốt's CDN, for a seller who
// brings their own listings to ShopNexus. It keeps the items and drops what
// places or reaches the seller: phone, address, coordinates and identity
// numbers are never decoded.
package chotot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrNotFound is a listing the gateway does not have, or has but not on
	// sale (hidden, expired, sold), or a profile it does not have.
	ErrNotFound = errors.New("chotot: no such listing on sale or profile")
	// ErrUnavailable is a gateway or CDN that did not answer or answered
	// 5xx, 403 or 429, or a gateway that redirected: try again later.
	ErrUnavailable = errors.New("chotot: did not answer")
	// ErrFormat is an answer whose shape the client does not know: Chợ Tốt
	// changed the gateway.
	ErrFormat = errors.New("chotot: the gateway answered in an unknown shape")
	// ErrBadImage is an answer from the CDN that is not a JPEG, PNG or WebP
	// of at most MaxImageBytes, or a photo it does not have.
	ErrBadImage = errors.New("chotot: not a photo on Chợ Tốt's CDN")
	// ErrOffCDN is a photo URL off CHOTOT_CDN_URL, which is never fetched.
	// It is an ErrBadImage too; every photo of a listing off the CDN means
	// Chợ Tốt moved its photos.
	ErrOffCDN = fmt.Errorf("%w: off the CDN", ErrBadImage)
)

const (
	// MaxImageBytes is the largest photo copied, the largest upload too.
	MaxImageBytes = 15 << 20
	// maxAdBytes caps a gateway answer; a listing is a few kilobytes, a page
	// of a seller's listings a few hundred.
	maxAdBytes = 2 << 20
	userAgent  = "ShopNexus-Chotot/1"
	// MaxAds is how many of a seller's listings Ads reads, newest first.
	MaxAds = 200
	// adsPage is how many listings one call of Ads asks for.
	adsPage = 50
	// RealEstateFrom and RealEstateTo bound Nhà Tốt's categories, real
	// estate, which ShopNexus does not sell.
	RealEstateFrom, RealEstateTo = 1000, 1999
)

// Param is one of the listing's structured fields, such as "Dung lượng":
// "64 GB".
type Param struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Ad is what ShopNexus keeps of a listing. Price is 0 when the listing has
// none. AccountOID names the seller's profile, so a caller checks whose
// listing it is.
type Ad struct {
	ListID       int64    `json:"list_id"`
	AccountOID   string   `json:"account_oid"`
	Category     int      `json:"category"`
	Subject      string   `json:"subject"`
	Body         string   `json:"body"`
	Price        int64    `json:"price"`
	CategoryName string   `json:"category_name"`
	Params       []Param  `json:"params"`
	Images       []string `json:"images"`
}

// Client reads Chợ Tốt. GatewayURL is CHOTOT_GATEWAY_URL; a photo is read
// only from the scheme and host of CDNURL, CHOTOT_CDN_URL.
type Client struct {
	GatewayURL string
	CDNURL     string
	HTTP       *http.Client
}

// placesSeller reports a parameter that says where the seller is.
func placesSeller(id string) bool {
	switch id {
	case "region", "area", "ward", "region_v2", "area_v2", "ward_v2", "street", "street_name", "house_number":
		return true
	}
	return strings.Contains(id, "address") || strings.Contains(id, "phone") || strings.Contains(id, "location")
}

// Ad reads listing listID, decoding the fields of Ad and no others.
func (c Client) Ad(ctx context.Context, listID int64) (Ad, error) {
	body, err := c.get(ctx, "/v1/public/ad-listing/"+strconv.FormatInt(listID, 10))
	if err != nil {
		return Ad{}, err
	}
	var raw struct {
		Ad struct {
			ListID       int64    `json:"list_id"`
			AccountOID   string   `json:"account_oid"`
			Category     int      `json:"category"`
			Subject      string   `json:"subject"`
			Body         string   `json:"body"`
			Price        int64    `json:"price"`
			CategoryName string   `json:"category_name"`
			Status       string   `json:"status"`
			Images       []string `json:"images"`
		} `json:"ad"`
		Parameters []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"parameters"`
	}
	if err = json.Unmarshal(body, &raw); err != nil {
		return Ad{}, fmt.Errorf("%w: %w", ErrFormat, err)
	}
	a := raw.Ad
	if a.Status != "" && a.Status != "active" {
		return Ad{}, ErrNotFound
	}
	if a.ListID != listID || strings.TrimSpace(a.Subject) == "" || len(a.Images) == 0 {
		return Ad{}, fmt.Errorf("%w: no list_id, subject or images", ErrFormat)
	}
	out := Ad{
		ListID: a.ListID, AccountOID: strings.ToLower(a.AccountOID), Category: a.Category, Subject: a.Subject, Body: a.Body, Price: a.Price,
		CategoryName: a.CategoryName, Images: a.Images, Params: []Param{},
	}
	for _, p := range raw.Parameters {
		if placesSeller(p.ID) || strings.TrimSpace(p.Label) == "" || strings.TrimSpace(p.Value) == "" {
			continue
		}
		out.Params = append(out.Params, Param{Label: p.Label, Value: p.Value})
	}
	return out, nil
}

// Profile is what ShopNexus keeps of a seller's profile. AccountID is the
// number Chợ Tốt lists the seller's listings by; Description is the
// profile's "Giới thiệu về trang", empty when the seller wrote none.
type Profile struct {
	OID         string `json:"oid"`
	AccountID   int64  `json:"account_id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
}

// Profile reads the profile oid names, decoding the fields of Profile and
// no others: never the phone, address or identity numbers it also holds.
func (c Client) Profile(ctx context.Context, oid string) (Profile, error) {
	body, err := c.get(ctx, "/v1/public/profile/"+url.PathEscape(oid))
	if err != nil {
		return Profile{}, err
	}
	var raw struct {
		AccountOID  string `json:"account_oid"`
		AccountID   int64  `json:"account_id"`
		FullName    string `json:"full_name"`
		Avatar      string `json:"avatar"`
		Description string `json:"description"`
	}
	if err = json.Unmarshal(body, &raw); err != nil {
		return Profile{}, fmt.Errorf("%w: %w", ErrFormat, err)
	}
	if !strings.EqualFold(raw.AccountOID, oid) || raw.AccountID <= 0 {
		return Profile{}, fmt.Errorf("%w: no account_oid or account_id", ErrFormat)
	}
	return Profile{
		OID: strings.ToLower(raw.AccountOID), AccountID: raw.AccountID, Name: strings.TrimSpace(raw.FullName),
		AvatarURL: raw.Avatar, Description: raw.Description,
	}, nil
}

// Listed is one of a seller's listings on sale as the gateway lists them.
// Thumb is the cover's small photo on the CDN.
type Listed struct {
	ListID       int64  `json:"list_id"`
	Subject      string `json:"subject"`
	Price        int64  `json:"price"`
	Category     int    `json:"category"`
	CategoryName string `json:"category_name"`
	Thumb        string `json:"thumb"`
}

// Ads reads the listings on sale of the seller Chợ Tốt numbers accountID,
// newest first, at most MaxAds. A listing of another profile than oid, or
// not active, is left out: the gateway's filter is not trusted alone.
func (c Client) Ads(ctx context.Context, accountID int64, oid string) ([]Listed, error) {
	out := []Listed{}
	for offset := 0; offset < MaxAds; offset += adsPage {
		body, err := c.get(ctx, fmt.Sprintf("/v1/public/ad-listing?account_id=%d&limit=%d&o=%d", accountID, adsPage, offset))
		if err != nil {
			return nil, err
		}
		var raw struct {
			Ads []struct {
				ListID       int64  `json:"list_id"`
				AccountOID   string `json:"account_oid"`
				Subject      string `json:"subject"`
				Price        int64  `json:"price"`
				Category     int    `json:"category"`
				CategoryName string `json:"category_name"`
				Image        string `json:"image"`
				Status       string `json:"status"`
			} `json:"ads"`
			Total *int `json:"total"`
		}
		if err = json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFormat, err)
		}
		if raw.Total == nil {
			return nil, fmt.Errorf("%w: no total", ErrFormat)
		}
		for _, a := range raw.Ads {
			if a.ListID <= 0 || !strings.EqualFold(a.AccountOID, oid) || a.Status != "active" || strings.TrimSpace(a.Subject) == "" {
				continue
			}
			out = append(out, Listed{
				ListID: a.ListID, Subject: a.Subject, Price: a.Price, Category: a.Category, CategoryName: a.CategoryName, Thumb: a.Image,
			})
		}
		if len(raw.Ads) < adsPage || offset+adsPage >= *raw.Total {
			break
		}
	}
	return out, nil
}

// RealEstate reports a category of Nhà Tốt's.
func RealEstate(category int) bool { return category >= RealEstateFrom && category <= RealEstateTo }

// get reads path on the gateway: 404 is ErrNotFound; any other answer but
// 200, a redirect or no answer is ErrUnavailable.
func (c Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.GatewayURL, "/")+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the gateway answered %d", ErrUnavailable, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAdBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(body) > maxAdBytes {
		return nil, fmt.Errorf("%w: over %d bytes", ErrFormat, maxAdBytes)
	}
	return body, nil
}

// Image reads one of a listing's photos and returns it with its content
// type, from its magic bytes.
func (c Client) Image(ctx context.Context, raw string) (photo []byte, contentType string, err error) {
	cdn, err := url.Parse(c.CDNURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse CHOTOT_CDN_URL: %w", err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != cdn.Scheme || !strings.EqualFold(u.Host, cdn.Host) || u.User != nil {
		return nil, "", ErrOffCDN
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := c.do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusForbidden:
		return nil, "", fmt.Errorf("%w: the CDN answered %d", ErrUnavailable, res.StatusCode)
	case res.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("%w: the CDN answered %d", ErrBadImage, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(b) == 0 || len(b) > MaxImageBytes {
		return nil, "", fmt.Errorf("%w: %d bytes", ErrBadImage, len(b))
	}
	switch ct := http.DetectContentType(b); ct {
	case "image/jpeg", "image/png", "image/webp":
		return b, ct, nil
	default:
		return nil, "", fmt.Errorf("%w: %s", ErrBadImage, ct)
	}
}

// do sends req without following a redirect: a 3xx comes back as the
// answer, so neither call reaches a host it was not given.
func (c Client) do(req *http.Request) (*http.Response, error) {
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// get asks CHOTOT_GATEWAY_URL, and Image only a URL whose scheme and host
	// it checked against CHOTOT_CDN_URL.
	res, err := hc.Do(req) //nolint:gosec // see above
	if err != nil {
		return nil, fmt.Errorf("send %s: %w", req.URL.Host, err)
	}
	return res, nil
}
