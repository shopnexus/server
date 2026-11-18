// Package litellm drives LiteLLM's admin API with the master key: the
// credentials that are providers, the deployments behind each model name,
// the router's settings, the keys and their budgets, and the daily spend.
// It knows nothing of agents or tags; module agent maps them.
package litellm

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
	"strings"
)

// ErrNotFound is LiteLLM's 404: no credential, deployment, key or budget
// of that name.
var ErrNotFound = errors.New("litellm: not found")

// ErrRefused is any other 4xx: LiteLLM refused the request as written, and
// the same request would be refused again. Error wraps it with the detail.
var ErrRefused = errors.New("litellm: refused")

// maxReply caps how much of an answer is read.
const maxReply = 8 << 20

// Admin is a client of LiteLLM's admin API. A caller bounds each call with
// its context.
type Admin struct {
	BaseURL   string
	MasterKey string
	HTTP      *http.Client
}

// Credential is a LiteLLM credential: values holds the api_key and
// api_base, info the fields LiteLLM keeps beside them. LiteLLM masks the
// values it answers with.
type Credential struct {
	Name        string            `json:"credential_name"`
	DisplayName string            `json:"display_name,omitempty"`
	Values      map[string]string `json:"credential_values,omitempty"`
	Info        map[string]any    `json:"credential_info"`
}

// Deployment is one entry of LiteLLM's model list: a model name callers
// send, the provider's model behind it and the credential that pays.
type Deployment struct {
	ModelName string         `json:"model_name"`
	Params    Params         `json:"litellm_params"`
	Info      map[string]any `json:"model_info,omitempty"`
}

// Params are the litellm_params the server writes and reads.
type Params struct {
	// Model is the provider route and the model, such as
	// openai/claude-haiku-5-5.
	Model      string   `json:"model"`
	Credential string   `json:"litellm_credential_name,omitempty"`
	Weight     *float64 `json:"weight,omitempty"`
	Order      *int     `json:"order,omitempty"`
	// InputCost and OutputCost are per token, in USD.
	InputCost  *float64 `json:"input_cost_per_token,omitempty"`
	OutputCost *float64 `json:"output_cost_per_token,omitempty"`
}

// ID is the deployment's id in model_info, which delete takes.
func (d Deployment) ID() string { return d.InfoString("id") }

// InfoString is a string field of model_info, or "".
func (d Deployment) InfoString(key string) string {
	if v, ok := d.Info[key].(string); ok {
		return v
	}
	return ""
}

// Key is a LiteLLM key as /key/list answers it with full objects. Token is
// the key's hash, which /key/update takes.
type Key struct {
	Token          string                 `json:"token"`
	Alias          string                 `json:"key_alias"`
	Models         []string               `json:"models"`
	Spend          float64                `json:"spend"`
	MaxBudget      *float64               `json:"max_budget"`
	BudgetDuration *string                `json:"budget_duration"`
	ModelMaxBudget map[string]ModelBudget `json:"model_max_budget"`
	ModelSpend     map[string]float64     `json:"model_spend"`
	EndUserBudget  *string                `json:"end_user_budget_id"`
}

// ModelBudget is a key's budget for one model name.
type ModelBudget struct {
	MaxBudget float64 `json:"max_budget"`
	Duration  string  `json:"budget_duration"`
}

// Budget is a LiteLLM budget, such as the one end users of a key share.
type Budget struct {
	ID       string   `json:"budget_id"`
	Max      *float64 `json:"max_budget"`
	Duration *string  `json:"budget_duration"`
}

// Day is one day of spend: the total and the spend of each model name.
type Day struct {
	Date    string             `json:"date"`
	Spend   float64            `json:"spend"`
	ByModel map[string]float64 `json:"by_model"`
}

// TestResult is /health/test_connection's answer.
type TestResult struct {
	OK    bool
	Error string
}

// Credentials lists every credential, values masked.
func (a Admin) Credentials(ctx context.Context) ([]Credential, error) {
	var out struct {
		Credentials []Credential `json:"credentials"`
	}
	if err := a.do(ctx, http.MethodGet, "/credentials", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Credentials, nil
}

// CreateCredential stores c.
func (a Admin) CreateCredential(ctx context.Context, c Credential) error {
	return a.do(ctx, http.MethodPost, "/credentials", nil, c, nil)
}

// UpdateCredential replaces c's info and, when c has values, its values.
func (a Admin) UpdateCredential(ctx context.Context, c Credential) error {
	return a.do(ctx, http.MethodPatch, "/credentials/"+url.PathEscape(c.Name), nil, c, nil)
}

// DeleteCredential deletes the credential name.
func (a Admin) DeleteCredential(ctx context.Context, name string) error {
	return a.do(ctx, http.MethodDelete, "/credentials/"+url.PathEscape(name), nil, nil, nil)
}

// Deployments lists LiteLLM's model list, a wildcard deployment expanded
// into one entry per model its provider lists.
func (a Admin) Deployments(ctx context.Context) ([]Deployment, error) {
	var out struct {
		Data []Deployment `json:"data"`
	}
	if err := a.do(ctx, http.MethodGet, "/model/info", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// AddDeployment adds d to the model list.
func (a Admin) AddDeployment(ctx context.Context, d Deployment) error {
	return a.do(ctx, http.MethodPost, "/model/new", nil, d, nil)
}

// DeleteDeployment deletes the deployment id; one already gone is no error.
func (a Admin) DeleteDeployment(ctx context.Context, id string) error {
	err := a.do(ctx, http.MethodPost, "/model/delete", nil, map[string]string{"id": id}, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// RouterSettings reads the router's current settings.
func (a Admin) RouterSettings(ctx context.Context) (map[string]any, error) {
	var out struct {
		Current map[string]any `json:"current_values"`
	}
	if err := a.do(ctx, http.MethodGet, "/router/settings", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Current == nil {
		out.Current = map[string]any{}
	}
	return out.Current, nil
}

// UpdateRouterSettings merges settings into the router's, which LiteLLM
// keeps in its database and applies on every replica.
func (a Admin) UpdateRouterSettings(ctx context.Context, settings map[string]any) error {
	return a.do(ctx, http.MethodPost, "/config/update", nil, map[string]any{"router_settings": settings}, nil)
}

// KeysByAlias lists the keys whose alias is alias.
func (a Admin) KeysByAlias(ctx context.Context, alias string) ([]Key, error) {
	var out struct {
		Keys []Key `json:"keys"`
	}
	q := url.Values{"key_alias": {alias}, "return_full_object": {"true"}, "size": {"100"}}
	if err := a.do(ctx, http.MethodGet, "/key/list", q, nil, &out); err != nil {
		return nil, err
	}
	keys := out.Keys[:0]
	for _, k := range out.Keys {
		if k.Alias == alias {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// UpdateKey sets fields of the key whose hash is token, such as models,
// max_budget, budget_duration, model_max_budget and end_user_budget_id; a
// nil value clears the field.
func (a Admin) UpdateKey(ctx context.Context, token string, fields map[string]any) error {
	body := maps.Clone(fields)
	if body == nil {
		body = map[string]any{}
	}
	body["key"] = token
	return a.do(ctx, http.MethodPost, "/key/update", nil, body, nil)
}

// Budget reads the budget id.
func (a Admin) Budget(ctx context.Context, id string) (Budget, error) {
	var out []Budget
	if err := a.do(ctx, http.MethodPost, "/budget/info", nil, map[string][]string{"budgets": {id}}, &out); err != nil {
		return Budget{}, err
	}
	for _, b := range out {
		if b.ID == id {
			return b, nil
		}
	}
	return Budget{}, ErrNotFound
}

// SaveBudget updates the budget b.ID, and creates it when LiteLLM has none.
func (a Admin) SaveBudget(ctx context.Context, b Budget) error {
	_, err := a.Budget(ctx, b.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return a.do(ctx, http.MethodPost, "/budget/new", nil, b, nil)
	case err != nil:
		return err
	default:
		return a.do(ctx, http.MethodPost, "/budget/update", nil, b, nil)
	}
}

// DailySpend is the spend of each day from start to end, both YYYY-MM-DD.
func (a Admin) DailySpend(ctx context.Context, start, end string) ([]Day, error) {
	var out struct {
		Results []struct {
			Date    string `json:"date"`
			Metrics struct {
				Spend float64 `json:"spend"`
			} `json:"metrics"`
			Breakdown struct {
				ModelGroups map[string]struct {
					Metrics struct {
						Spend float64 `json:"spend"`
					} `json:"metrics"`
				} `json:"model_groups"`
			} `json:"breakdown"`
		} `json:"results"`
	}
	q := url.Values{"start_date": {start}, "end_date": {end}}
	if err := a.do(ctx, http.MethodGet, "/user/daily/activity/aggregated", q, nil, &out); err != nil {
		return nil, err
	}
	days := make([]Day, 0, len(out.Results))
	for _, r := range out.Results {
		d := Day{Date: r.Date, Spend: r.Metrics.Spend, ByModel: map[string]float64{}}
		for name, m := range r.Breakdown.ModelGroups {
			d.ByModel[name] = m.Metrics.Spend
		}
		days = append(days, d)
	}
	return days, nil
}

// TestConnection asks LiteLLM to call model with the credential once.
func (a Admin) TestConnection(ctx context.Context, model, credential, mode string) (TestResult, error) {
	var out struct {
		Status string `json:"status"`
		Error  any    `json:"error"`
		Result any    `json:"result"`
	}
	body := map[string]any{"mode": mode, "litellm_params": Params{Model: model, Credential: credential}}
	if err := a.do(ctx, http.MethodPost, "/health/test_connection", nil, body, &out); err != nil {
		if errors.Is(err, ErrRefused) {
			return TestResult{OK: false, Error: strings.TrimPrefix(err.Error(), ErrRefused.Error()+": ")}, nil
		}
		return TestResult{}, err
	}
	if out.Status == "success" {
		return TestResult{OK: true}, nil
	}
	msg := fmt.Sprint(out.Error)
	if out.Error == nil {
		msg = fmt.Sprint(out.Result)
	}
	return TestResult{OK: false, Error: msg}, nil
}

// do sends one request with the master key and decodes a 2xx answer into
// out when out is not nil. A 404 is ErrNotFound and another 4xx
// ErrRefused, with LiteLLM's detail; anything else is a plain error.
func (a Admin) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode litellm %s: %w", path, err)
		}
		body = bytes.NewReader(b)
	}
	target := strings.TrimSuffix(a.BaseURL, "/") + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("build litellm %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.MasterKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("litellm %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReply))
	if err != nil {
		return fmt.Errorf("read litellm %s %s: %w", method, path, err)
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s %s", ErrNotFound, method, path)
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return fmt.Errorf("%w: %s", ErrRefused, detail(raw))
	case res.StatusCode < 200 || res.StatusCode > 299:
		return fmt.Errorf("litellm %s %s answered %d", method, path, res.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode litellm %s %s: %w", method, path, err)
	}
	return nil
}

// detail is LiteLLM's error message: detail.error, error.message or the
// body itself, at most 300 bytes.
func detail(raw []byte) string {
	var body struct {
		Detail any `json:"detail"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &body) == nil {
		switch d := body.Detail.(type) {
		case string:
			msg = d
		case map[string]any:
			if e, ok := d["error"]; ok {
				msg = fmt.Sprint(e)
			}
		default:
			if body.Error.Message != "" {
				msg = body.Error.Message
			}
		}
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
