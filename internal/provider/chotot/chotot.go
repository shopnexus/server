// Package chotot reads one listing from Chợ Tốt's public gateway and its
// photos from Chợ Tốt's CDN, for a seller who copies their own listing to
// ShopNexus. It keeps the item and drops the seller: the name, phone,
// address and coordinates are never decoded.
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
	// sale (hidden, expired, sold).
	ErrNotFound = errors.New("chotot: no such listing on sale")
	// ErrUnavailable is a gateway or CDN that did not answer, answered 5xx,
	// 403 or 429, or redirected: try again later.
	ErrUnavailable = errors.New("chotot: did not answer")
	// ErrFormat is an answer whose shape the client does not know: Chợ Tốt
	// changed the gateway.
	ErrFormat = errors.New("chotot: the gateway answered in an unknown shape")
	// ErrBadImage is a photo URL off the CDN, or an answer that is not a
	// JPEG, PNG or WebP of at most MaxImageBytes.
	ErrBadImage = errors.New("chotot: not a photo on Chợ Tốt's CDN")
)

const (
	// MaxImageBytes is the largest photo copied, the largest upload too.
	MaxImageBytes = 15 << 20
	// maxAdBytes caps a gateway answer; a listing is a few kilobytes.
	maxAdBytes = 1 << 20
	userAgent  = "ShopNexus-LinkRead/1"
)

// Param is one of the listing's structured fields, such as "Dung lượng":
// "64 GB".
type Param struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Ad is what a link read keeps of a listing. Price is 0 when the listing
// has none.
type Ad struct {
	ListID       int64    `json:"list_id"`
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.GatewayURL, "/")+"/v1/public/ad-listing/"+strconv.FormatInt(listID, 10), http.NoBody)
	if err != nil {
		return Ad{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := c.do(req)
	if err != nil {
		return Ad{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return Ad{}, ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return Ad{}, fmt.Errorf("%w: the gateway answered %d", ErrUnavailable, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAdBytes+1))
	if err != nil {
		return Ad{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(body) > maxAdBytes {
		return Ad{}, fmt.Errorf("%w: over %d bytes", ErrFormat, maxAdBytes)
	}
	var raw struct {
		Ad struct {
			ListID       int64    `json:"list_id"`
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
		ListID: a.ListID, Subject: a.Subject, Body: a.Body, Price: a.Price, CategoryName: a.CategoryName,
		Images: a.Images, Params: []Param{},
	}
	for _, p := range raw.Parameters {
		if placesSeller(p.ID) || strings.TrimSpace(p.Label) == "" || strings.TrimSpace(p.Value) == "" {
			continue
		}
		out.Params = append(out.Params, Param{Label: p.Label, Value: p.Value})
	}
	return out, nil
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
		return nil, "", ErrBadImage
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
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
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
	// Ad asks CHOTOT_GATEWAY_URL, and Image only a URL whose scheme and host
	// it checked against CHOTOT_CDN_URL.
	res, err := hc.Do(req) //nolint:gosec // see above
	if err != nil {
		return nil, fmt.Errorf("send %s: %w", req.URL.Host, err)
	}
	return res, nil
}
