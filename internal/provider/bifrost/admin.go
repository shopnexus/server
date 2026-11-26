// Package bifrost is the AI gateway's client: Admin drives Bifrost's admin
// API with Basic auth (teams, virtual keys, pricing overrides, providers,
// the model catalog and the logs), Ring keeps the server's virtual keys in
// memory, and Chat and Embedder call models through the gateway with a
// caller's key. It knows nothing of agents; module agent maps them.
package bifrost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrRefused is a 4xx of the admin API: the same request would be refused
// again. An *Error wraps it with Bifrost's message.
var ErrRefused = errors.New("bifrost: refused")

// Error is an answer other than 2xx, with the message Bifrost gave.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("bifrost answered %d: %s", e.Status, e.Message)
}

func (e *Error) Is(target error) bool {
	return target == ErrRefused && e.Status >= 400 && e.Status < 500
}

// maxReply caps how much of an answer is read.
const maxReply = 16 << 20

// Admin is a client of Bifrost's admin API. A caller bounds each call with
// its context.
type Admin struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

// Budget is a spending limit in USD that resets every ResetDuration
// ("1M" is a month).
type Budget struct {
	MaxLimit      float64 `json:"max_limit"`
	ResetDuration string  `json:"reset_duration"`
	CurrentUsage  float64 `json:"current_usage,omitempty"`
	// LastReset is when the window of CurrentUsage opened.
	LastReset time.Time `json:"last_reset,omitzero"`
}

// RateLimit is a limit of tokens and of requests, each per its window.
type RateLimit struct {
	TokenMaxLimit        *int64  `json:"token_max_limit,omitempty"`
	TokenResetDuration   *string `json:"token_reset_duration,omitempty"`
	RequestMaxLimit      *int64  `json:"request_max_limit,omitempty"`
	RequestResetDuration *string `json:"request_reset_duration,omitempty"`
}

// Team groups virtual keys under its budgets.
type Team struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Budgets         []Budget `json:"budgets"`
	CalendarAligned bool     `json:"calendar_aligned"`
}

// ProviderConfig is one provider a virtual key may call and its models.
type ProviderConfig struct {
	Provider      string   `json:"provider"`
	AllowedModels []string `json:"allowed_models"`
	AllowAllKeys  bool     `json:"allow_all_keys"`
}

// VirtualKey is a caller's key: Value is what the caller sends.
type VirtualKey struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Value           string            `json:"value"`
	TeamID          *string           `json:"team_id"`
	IsActive        *bool             `json:"is_active"`
	ProviderConfigs []ProviderConfig  `json:"provider_configs"`
	RateLimit       *RateLimit        `json:"rate_limit"`
	Metadata        map[string]string `json:"metadata"`
}

// Active says the key may call: is_active, true when unset.
func (k VirtualKey) Active() bool { return k.IsActive == nil || *k.IsActive }

// KeyInput is what a virtual key is written with. Rate's nil fields are
// no limit.
type KeyInput struct {
	Name     string
	TeamID   string
	Models   map[string][]string // allowed models by provider
	Rate     RateLimit
	Active   bool
	Metadata map[string]string
}

// Price is a model's price in USD per token. A nil cache price is the
// input price.
type Price struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

// PricingOverride prices a provider's model exactly, for chat and
// embedding requests.
type PricingOverride struct {
	ID       string
	Name     string
	Provider string
	Pattern  string
	// Patch is the override's fields as Bifrost keeps them.
	Patch map[string]float64
}

// PricePatch is the pricing override's patch of p: the cache prices fall
// back to the input price, so the catalog's never apply.
func PricePatch(p Price) map[string]float64 {
	out := map[string]float64{
		"input_cost_per_token": p.Input, "output_cost_per_token": p.Output,
		"cache_read_input_token_cost": p.Input, "cache_creation_input_token_cost": p.Input,
	}
	if p.CacheRead != nil {
		out["cache_read_input_token_cost"] = *p.CacheRead
	}
	if p.CacheWrite != nil {
		out["cache_creation_input_token_cost"] = *p.CacheWrite
	}
	return out
}

// Provider is a provider of config.json as Bifrost runs it.
type Provider struct {
	Name    string
	BaseURL string
	// Status is Bifrost's: active, or error when it failed to start.
	Status string
	// Custom is a custom provider's base type, such as openai; empty for a
	// built-in one.
	Custom string
}

// Model is a model of Bifrost's catalog, with its list price per token.
type Model struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	MaxInput   *int     `json:"max_input_tokens"`
	Input      *float64 `json:"input_cost_per_token"`
	Output     *float64 `json:"output_cost_per_token"`
	CacheRead  *float64 `json:"cache_read_input_token_cost"`
	CacheWrite *float64 `json:"cache_creation_input_token_cost"`
}

// Teams lists every team.
func (a Admin) Teams(ctx context.Context) ([]Team, error) {
	var out struct {
		Teams []Team `json:"teams"`
	}
	if err := a.do(ctx, http.MethodGet, "/api/governance/teams", nil, &out); err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	return out.Teams, nil
}

// teamBody is a team with a monthly budget, or none for nil, that resets on
// the first of each month.
func teamBody(name string, monthlyUSD *float64) map[string]any {
	budgets := []Budget{}
	if monthlyUSD != nil {
		budgets = append(budgets, Budget{MaxLimit: *monthlyUSD, ResetDuration: "1M"})
	}
	return map[string]any{"name": name, "budgets": budgets, "calendar_aligned": true}
}

// CreateTeam makes a team with a monthly budget, nil for none.
func (a Admin) CreateTeam(ctx context.Context, name string, monthlyUSD *float64) (Team, error) {
	var out struct {
		Team Team `json:"team"`
	}
	if err := a.do(ctx, http.MethodPost, "/api/governance/teams", teamBody(name, monthlyUSD), &out); err != nil {
		return Team{}, fmt.Errorf("create team %s: %w", name, err)
	}
	return out.Team, nil
}

// UpdateTeam sets a team's monthly budget, nil for none; its spend so far
// stays.
func (a Admin) UpdateTeam(ctx context.Context, id, name string, monthlyUSD *float64) error {
	if err := a.do(ctx, http.MethodPut, "/api/governance/teams/"+url.PathEscape(id), teamBody(name, monthlyUSD), nil); err != nil {
		return fmt.Errorf("update team %s: %w", name, err)
	}
	return nil
}

// DeleteTeam deletes a team.
func (a Admin) DeleteTeam(ctx context.Context, id string) error {
	if err := a.do(ctx, http.MethodDelete, "/api/governance/teams/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("delete team %s: %w", id, err)
	}
	return nil
}

// VirtualKeys lists every virtual key with its value.
func (a Admin) VirtualKeys(ctx context.Context) ([]VirtualKey, error) {
	var out struct {
		Keys []VirtualKey `json:"virtual_keys"`
	}
	if err := a.do(ctx, http.MethodGet, "/api/governance/virtual-keys", nil, &out); err != nil {
		return nil, fmt.Errorf("list virtual keys: %w", err)
	}
	return out.Keys, nil
}

// keyBody is in as Bifrost takes it. Each provider config takes every
// provider key (key_ids *), since none is allowed otherwise; an update
// with no rate limit sends {}, which removes it.
func keyBody(in KeyInput, update bool) map[string]any {
	configs := make([]map[string]any, 0, len(in.Models))
	for _, provider := range slices.Sorted(maps.Keys(in.Models)) {
		configs = append(configs, map[string]any{"provider": provider, "allowed_models": in.Models[provider], "key_ids": []string{"*"}})
	}
	body := map[string]any{
		"name": in.Name, "team_id": in.TeamID, "provider_configs": configs, "is_active": in.Active, "metadata": in.Metadata,
	}
	if in.Rate != (RateLimit{}) || update {
		body["rate_limit"] = in.Rate
	}
	return body
}

// CreateVirtualKey makes a key; Bifrost makes its value.
func (a Admin) CreateVirtualKey(ctx context.Context, in KeyInput) (VirtualKey, error) {
	var out struct {
		Key VirtualKey `json:"virtual_key"`
	}
	if err := a.do(ctx, http.MethodPost, "/api/governance/virtual-keys", keyBody(in, false), &out); err != nil {
		return VirtualKey{}, fmt.Errorf("create virtual key %s: %w", in.Name, err)
	}
	return out.Key, nil
}

// UpdateVirtualKey writes in over the key; its value stays.
func (a Admin) UpdateVirtualKey(ctx context.Context, id string, in KeyInput) error {
	if err := a.do(ctx, http.MethodPut, "/api/governance/virtual-keys/"+url.PathEscape(id), keyBody(in, true), nil); err != nil {
		return fmt.Errorf("update virtual key %s: %w", in.Name, err)
	}
	return nil
}

// DeleteVirtualKey deletes a key.
func (a Admin) DeleteVirtualKey(ctx context.Context, id string) error {
	if err := a.do(ctx, http.MethodDelete, "/api/governance/virtual-keys/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("delete virtual key %s: %w", id, err)
	}
	return nil
}

// PricingOverrides lists every pricing override.
func (a Admin) PricingOverrides(ctx context.Context) ([]PricingOverride, error) {
	var out struct {
		Overrides []struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			ProviderID *string `json:"provider_id"`
			Pattern    string  `json:"pattern"`
			// Patch is a JSON object in a string.
			Patch string `json:"pricing_patch"`
		} `json:"pricing_overrides"`
	}
	if err := a.do(ctx, http.MethodGet, "/api/governance/pricing-overrides", nil, &out); err != nil {
		return nil, fmt.Errorf("list pricing overrides: %w", err)
	}
	list := make([]PricingOverride, 0, len(out.Overrides))
	for _, o := range out.Overrides {
		p := PricingOverride{ID: o.ID, Name: o.Name, Pattern: o.Pattern, Patch: map[string]float64{}}
		if o.ProviderID != nil {
			p.Provider = *o.ProviderID
		}
		// A patch that does not decode is one to write again.
		_ = json.Unmarshal([]byte(o.Patch), &p.Patch)
		list = append(list, p)
	}
	return list, nil
}

func overrideBody(name, provider, model string, p Price) map[string]any {
	return map[string]any{
		"name": name, "scope_kind": "provider", "provider_id": provider, "match_type": "exact", "pattern": model,
		"request_types": []string{"chat_completion", "embedding"}, "patch": PricePatch(p),
	}
}

// CreatePricingOverride prices provider's model exactly.
func (a Admin) CreatePricingOverride(ctx context.Context, name, provider, model string, p Price) error {
	if err := a.do(ctx, http.MethodPost, "/api/governance/pricing-overrides", overrideBody(name, provider, model, p), nil); err != nil {
		return fmt.Errorf("create pricing override %s: %w", name, err)
	}
	return nil
}

// UpdatePricingOverride writes the override again.
func (a Admin) UpdatePricingOverride(ctx context.Context, id, name, provider, model string, p Price) error {
	if err := a.do(ctx, http.MethodPut, "/api/governance/pricing-overrides/"+url.PathEscape(id),
		overrideBody(name, provider, model, p), nil); err != nil {
		return fmt.Errorf("update pricing override %s: %w", name, err)
	}
	return nil
}

// DeletePricingOverride deletes an override.
func (a Admin) DeletePricingOverride(ctx context.Context, id string) error {
	if err := a.do(ctx, http.MethodDelete, "/api/governance/pricing-overrides/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("delete pricing override %s: %w", id, err)
	}
	return nil
}

// Providers lists the providers Bifrost runs.
func (a Admin) Providers(ctx context.Context) ([]Provider, error) {
	var out struct {
		Providers []struct {
			Name          string `json:"name"`
			NetworkConfig struct {
				BaseURL string `json:"base_url"`
			} `json:"network_config"`
			Status string `json:"provider_status"`
			Custom *struct {
				Base string `json:"base_provider_type"`
			} `json:"custom_provider_config"`
		} `json:"providers"`
	}
	if err := a.do(ctx, http.MethodGet, "/api/providers", nil, &out); err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	list := make([]Provider, 0, len(out.Providers))
	for _, p := range out.Providers {
		v := Provider{Name: p.Name, BaseURL: p.NetworkConfig.BaseURL, Status: p.Status}
		if p.Custom != nil {
			v.Custom = p.Custom.Base
		}
		list = append(list, v)
	}
	return list, nil
}

// Models lists up to limit models of provider's catalog, and how many it
// has in all.
func (a Admin) Models(ctx context.Context, provider string, limit int) ([]Model, int, error) {
	var out struct {
		Models []Model `json:"models"`
		Total  int     `json:"total"`
	}
	q := url.Values{"provider": {provider}, "limit": {strconv.Itoa(limit)}}
	if err := a.do(ctx, http.MethodGet, "/api/models/details?"+q.Encode(), nil, &out); err != nil {
		return nil, 0, fmt.Errorf("list %s's models: %w", provider, err)
	}
	return out.Models, out.Total, nil
}

// CostBucket is the cost logged in one bucket of time, by team ID.
type CostBucket struct {
	At     time.Time          `json:"timestamp"`
	Total  float64            `json:"total_cost"`
	ByTeam map[string]float64 `json:"by_dimension"`
}

// CostByTeam is the logged cost from from to to, by team, in buckets
// whose size Bifrost picks from the window: a day for 7 to 31 days, less
// for a shorter one.
func (a Admin) CostByTeam(ctx context.Context, from, to time.Time) ([]CostBucket, error) {
	var out struct {
		Buckets []CostBucket `json:"buckets"`
	}
	q := window(from, to)
	q.Set("dimension", "team_id")
	if err := a.do(ctx, http.MethodGet, "/api/logs/histogram/cost/by-dimension?"+q.Encode(), nil, &out); err != nil {
		return nil, fmt.Errorf("read the cost by team: %w", err)
	}
	return out.Buckets, nil
}

// CountBucket is the requests logged in one bucket of time.
type CountBucket struct {
	At    time.Time `json:"timestamp"`
	Count int       `json:"count"`
}

// Requests is the requests of a team logged from from to to, in buckets
// as CostByTeam's.
func (a Admin) Requests(ctx context.Context, teamID string, from, to time.Time) ([]CountBucket, error) {
	var out struct {
		Buckets []CountBucket `json:"buckets"`
	}
	q := window(from, to)
	q.Set("team_ids", teamID)
	if err := a.do(ctx, http.MethodGet, "/api/logs/histogram?"+q.Encode(), nil, &out); err != nil {
		return nil, fmt.Errorf("read the requests of team %s: %w", teamID, err)
	}
	return out.Buckets, nil
}

// Stats is a window's requests and the percentage that succeeded.
type Stats struct {
	Requests    int     `json:"total_requests"`
	SuccessRate float64 `json:"success_rate"`
}

// ProviderStats is provider's requests from from to to.
func (a Admin) ProviderStats(ctx context.Context, provider string, from, to time.Time) (Stats, error) {
	var out Stats
	q := window(from, to)
	q.Set("providers", provider)
	if err := a.do(ctx, http.MethodGet, "/api/logs/stats?"+q.Encode(), nil, &out); err != nil {
		return Stats{}, fmt.Errorf("read %s's stats: %w", provider, err)
	}
	return out, nil
}

func window(from, to time.Time) url.Values {
	return url.Values{"start_time": {from.UTC().Format(time.RFC3339)}, "end_time": {to.UTC().Format(time.RFC3339)}}
}

// do sends body as JSON, when not nil, and decodes a 2xx answer into out,
// when not nil.
func (a Admin) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.BaseURL, "/")+path, rd)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(a.Username, a.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call bifrost: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReply))
	if err != nil {
		return fmt.Errorf("read the answer: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &Error{Status: res.StatusCode, Message: messageOf(raw)}
	}
	if out == nil {
		return nil
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode the answer: %w", err)
	}
	return nil
}

// messageOf is the message of Bifrost's error body, else its first 300
// bytes.
func messageOf(raw []byte) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 300 {
		msg = strings.ToValidUTF8(msg[:300], "")
	}
	return msg
}
