package service_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shopnexus/server/internal/provider/litellm"
)

const masterKey = "sk-master-test"

// fakeLiteLLM is LiteLLM's admin API as the server uses it, in memory:
// credentials, the model list with wildcard listers expanded, keys,
// budgets, router settings and daily spend. It refuses a call without the
// master key.
type fakeLiteLLM struct {
	srv    *httptest.Server
	mu     sync.Mutex
	creds  map[string]litellm.Credential
	deps   []litellm.Deployment
	next   int
	keys   []map[string]any
	budget map[string]map[string]any
	router map[string]any
	// listed is what each provider's lister expands into.
	listed map[string][]string
	days   []map[string]any
	// customerDays are the end users' daily activity, customers each end
	// user's budget, and logs the spend logs.
	customerDays []map[string]any
	customers    map[string]string
	logs         []map[string]any
	down         bool
}

func newFakeLiteLLM(t *testing.T) *fakeLiteLLM {
	t.Helper()
	f := &fakeLiteLLM{
		creds: map[string]litellm.Credential{}, budget: map[string]map[string]any{}, listed: map[string][]string{},
		customers: map[string]string{},
		router:    map[string]any{"routing_strategy": "simple-shuffle", "num_retries": 2.0},
		keys: []map[string]any{
			{
				"token": "hash-agent", "key_alias": "agent", "models": []any{"console-agent"}, "spend": 1.5,
				"model_spend": map[string]any{"console-agent": 1.25},
			},
			{"token": "hash-trust", "key_alias": "server-trust", "models": []any{"scam-check"}, "spend": 0.75},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /credentials", f.listCredentials)
	mux.HandleFunc("POST /credentials", f.createCredential)
	mux.HandleFunc("PATCH /credentials/{name}", f.updateCredential)
	mux.HandleFunc("DELETE /credentials/{name}", f.deleteCredential)
	mux.HandleFunc("GET /model/info", f.modelInfo)
	mux.HandleFunc("POST /model/new", f.modelNew)
	mux.HandleFunc("POST /model/delete", f.modelDelete)
	mux.HandleFunc("GET /router/settings", f.routerSettings)
	mux.HandleFunc("POST /config/update", f.configUpdate)
	mux.HandleFunc("GET /key/list", f.keyList)
	mux.HandleFunc("POST /key/update", f.keyUpdate)
	mux.HandleFunc("POST /budget/info", f.budgetInfo)
	mux.HandleFunc("POST /budget/new", f.budgetSave)
	mux.HandleFunc("POST /budget/update", f.budgetSave)
	mux.HandleFunc("GET /user/daily/activity/aggregated", f.daily)
	mux.HandleFunc("GET /customer/daily/activity/aggregated", f.daily)
	mux.HandleFunc("POST /customer/update", f.customerSave)
	mux.HandleFunc("POST /customer/new", f.customerSave)
	mux.HandleFunc("GET /spend/logs/v2", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"data": f.logs, "total_pages": 1})
	})
	mux.HandleFunc("POST /health/test_connection", f.testConnection)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+masterKey {
			reply(w, http.StatusUnauthorized, map[string]any{"detail": "no master key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLiteLLM) admin() litellm.Admin {
	return litellm.Admin{BaseURL: f.srv.URL, MasterKey: masterKey, HTTP: &http.Client{}}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) { _ = json.NewDecoder(r.Body).Decode(v) }

func (f *fakeLiteLLM) listCredentials(w http.ResponseWriter, _ *http.Request) {
	out := make([]litellm.Credential, 0, len(f.creds))
	for _, name := range slices.Sorted(maps.Keys(f.creds)) {
		c := f.creds[name]
		masked := map[string]string{}
		for k, v := range c.Values {
			masked[k] = v[:min(3, len(v))] + "****"
		}
		c.Values = masked
		out = append(out, c)
	}
	reply(w, http.StatusOK, map[string]any{"success": true, "credentials": out})
}

func (f *fakeLiteLLM) createCredential(w http.ResponseWriter, r *http.Request) {
	var c litellm.Credential
	decodeBody(r, &c)
	if _, ok := f.creds[c.Name]; ok {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "exists"})
		return
	}
	f.creds[c.Name] = c
	reply(w, http.StatusOK, map[string]any{"success": true})
}

func (f *fakeLiteLLM) updateCredential(w http.ResponseWriter, r *http.Request) {
	c, ok := f.creds[r.PathValue("name")]
	if !ok {
		reply(w, http.StatusNotFound, map[string]any{"detail": "no credential"})
		return
	}
	var patch litellm.Credential
	decodeBody(r, &patch)
	if patch.DisplayName != "" {
		c.DisplayName = patch.DisplayName
	}
	maps.Copy(c.Values, patch.Values)
	maps.Copy(c.Info, patch.Info)
	f.creds[c.Name] = c
	reply(w, http.StatusOK, map[string]any{"success": true})
}

func (f *fakeLiteLLM) deleteCredential(w http.ResponseWriter, r *http.Request) {
	delete(f.creds, r.PathValue("name"))
	reply(w, http.StatusOK, map[string]any{"success": true})
}

func (f *fakeLiteLLM) modelInfo(w http.ResponseWriter, _ *http.Request) {
	out := []litellm.Deployment{}
	for _, d := range f.deps {
		provider, ok := strings.CutSuffix(d.ModelName, "/*")
		if !ok {
			out = append(out, d)
			continue
		}
		for _, m := range f.listed[provider] {
			e := d
			e.ModelName = provider + "/" + m
			// LiteLLM fills a listed model's model_info from its cost map.
			e.Info = maps.Clone(d.Info)
			e.Info["max_input_tokens"] = 200000.0
			e.Info["input_cost_per_token"], e.Info["output_cost_per_token"] = 0.000003, 0.000015
			out = append(out, e)
		}
	}
	reply(w, http.StatusOK, map[string]any{"data": out})
}

func (f *fakeLiteLLM) modelNew(w http.ResponseWriter, r *http.Request) {
	var d litellm.Deployment
	decodeBody(r, &d)
	f.next++
	if d.Info == nil {
		d.Info = map[string]any{}
	}
	d.Info["id"] = fmt.Sprintf("dep-%d", f.next)
	// As LiteLLM keeps a deployment it stores: a price is the one in
	// litellm_params, which model_info shows too, and a price in
	// model_info alone is dropped, so model_info shows 0.
	for key, price := range map[string]*float64{
		"input_cost_per_token": d.Params.InputCost, "output_cost_per_token": d.Params.OutputCost,
	} {
		d.Info[key] = 0.0
		if price != nil {
			d.Info[key] = *price
		}
	}
	f.deps = append(f.deps, d)
	reply(w, http.StatusOK, d)
}

func (f *fakeLiteLLM) modelDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	decodeBody(r, &in)
	n := len(f.deps)
	f.deps = slices.DeleteFunc(f.deps, func(d litellm.Deployment) bool { return d.ID() == in.ID })
	if len(f.deps) == n {
		reply(w, http.StatusNotFound, map[string]any{"detail": "no model"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"message": "deleted"})
}

func (f *fakeLiteLLM) routerSettings(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"fields": []any{}, "current_values": f.router})
}

func (f *fakeLiteLLM) configUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RouterSettings map[string]any `json:"router_settings"`
	}
	decodeBody(r, &in)
	maps.Copy(f.router, in.RouterSettings)
	reply(w, http.StatusOK, map[string]any{"message": "updated"})
}

func (f *fakeLiteLLM) keyList(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, k := range f.keys {
		if k["key_alias"] == r.URL.Query().Get("key_alias") {
			out = append(out, k)
		}
	}
	reply(w, http.StatusOK, map[string]any{"keys": out})
}

func (f *fakeLiteLLM) keyUpdate(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	decodeBody(r, &in)
	for _, k := range f.keys {
		if k["token"] == in["key"] {
			for field, v := range in {
				if field != "key" {
					k[field] = v
				}
			}
			reply(w, http.StatusOK, k)
			return
		}
	}
	reply(w, http.StatusNotFound, map[string]any{"detail": "no key"})
}

func (f *fakeLiteLLM) budgetInfo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Budgets []string `json:"budgets"`
	}
	decodeBody(r, &in)
	out := []map[string]any{}
	for _, id := range in.Budgets {
		if b, ok := f.budget[id]; ok {
			out = append(out, b)
		}
	}
	reply(w, http.StatusOK, out)
}

func (f *fakeLiteLLM) budgetSave(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	decodeBody(r, &in)
	id, ok := in["budget_id"].(string)
	if !ok {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "budget_id"})
		return
	}
	if _, exists := f.budget[id]; exists == (r.URL.Path == "/budget/new") {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "budget exists or is missing"})
		return
	}
	f.budget[id] = in
	reply(w, http.StatusOK, in)
}

func (f *fakeLiteLLM) daily(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("start_date") == "" || r.URL.Query().Get("end_date") == "" {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "dates"})
		return
	}
	days := f.days
	if strings.HasPrefix(r.URL.Path, "/customer") {
		days = f.customerDays
	}
	reply(w, http.StatusOK, map[string]any{"results": days, "metadata": map[string]any{}})
}

// customerSave sets an end user's budget; /customer/update of one LiteLLM
// has not seen is 400, as LiteLLM answers.
func (f *fakeLiteLLM) customerSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID   string `json:"user_id"`
		BudgetID string `json:"budget_id"`
	}
	decodeBody(r, &in)
	if _, ok := f.customers[in.UserID]; ok == (r.URL.Path == "/customer/new") {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "end user exists or is missing"})
		return
	}
	if _, ok := f.budget[in.BudgetID]; !ok {
		reply(w, http.StatusBadRequest, map[string]any{"detail": "no budget"})
		return
	}
	f.customers[in.UserID] = in.BudgetID
	reply(w, http.StatusOK, map[string]any{"user_id": in.UserID})
}

// customer is the budget of end user id.
func (f *fakeLiteLLM) customer(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.customers[id]
}

func (f *fakeLiteLLM) testConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Params litellm.Params `json:"litellm_params"`
	}
	decodeBody(r, &in)
	if c, ok := f.creds[in.Params.Credential]; ok && c.Values["api_key"] != "bad" {
		reply(w, http.StatusOK, map[string]any{"status": "success", "result": map[string]any{}})
		return
	}
	reply(w, http.StatusOK, map[string]any{"status": "error", "error": "AuthenticationError: bad key"})
}

// aliases lists the deployments of model name alias as provider:weight:order.
func (f *fakeLiteLLM) aliases(alias string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, d := range f.deps {
		if d.ModelName == alias {
			out = append(out, fmt.Sprintf("%s:%s:%g:%d", d.Params.Credential, d.Params.Model, *d.Params.Weight, *d.Params.Order))
		}
	}
	slices.Sort(out)
	return out
}

// key reads the key with alias.
func (f *fakeLiteLLM) key(alias string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if k["key_alias"] == alias {
			return maps.Clone(k)
		}
	}
	return nil
}
