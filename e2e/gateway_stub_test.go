package e2e

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"

	"github.com/shopnexus/server/internal/provider/litellm"
)

const gatewayMasterKey = "e2e-litellm-master-key"

// gatewayRefused is a key or a model the stub refuses, as LiteLLM refuses
// a request it cannot take.
const gatewayRefused = "refuse-me"

// fakeLiteLLM stands in for LiteLLM's admin API as the server uses it, in memory:
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
	// customers is each end user's budget.
	customers map[string]string
	down      bool
}

func newGateway() *fakeLiteLLM {
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
		gwReply(w, http.StatusOK, map[string]any{"data": []any{}, "total_pages": 1})
	})
	mux.HandleFunc("POST /health/test_connection", f.testConnection)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+gatewayMasterKey {
			gwReply(w, http.StatusUnauthorized, map[string]any{"detail": "no master key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))

	return f
}

func (f *fakeLiteLLM) url() string { return f.srv.URL }

func gwReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func gwDecode(r *http.Request, v any) { _ = json.NewDecoder(r.Body).Decode(v) }

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
	gwReply(w, http.StatusOK, map[string]any{"success": true, "credentials": out})
}

func (f *fakeLiteLLM) createCredential(w http.ResponseWriter, r *http.Request) {
	var c litellm.Credential
	gwDecode(r, &c)
	if c.Values["api_key"] == gatewayRefused {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "invalid key"})
		return
	}
	if _, ok := f.creds[c.Name]; ok {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "exists"})
		return
	}
	f.creds[c.Name] = c
	gwReply(w, http.StatusOK, map[string]any{"success": true})
}

func (f *fakeLiteLLM) updateCredential(w http.ResponseWriter, r *http.Request) {
	c, ok := f.creds[r.PathValue("name")]
	if !ok {
		gwReply(w, http.StatusNotFound, map[string]any{"detail": "no credential"})
		return
	}
	var patch litellm.Credential
	gwDecode(r, &patch)
	if patch.Values["api_key"] == gatewayRefused {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "invalid key"})
		return
	}
	if patch.DisplayName != "" {
		c.DisplayName = patch.DisplayName
	}
	maps.Copy(c.Values, patch.Values)
	maps.Copy(c.Info, patch.Info)
	f.creds[c.Name] = c
	gwReply(w, http.StatusOK, map[string]any{"success": true})
}

func (f *fakeLiteLLM) deleteCredential(w http.ResponseWriter, r *http.Request) {
	delete(f.creds, r.PathValue("name"))
	gwReply(w, http.StatusOK, map[string]any{"success": true})
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
			out = append(out, e)
		}
	}
	gwReply(w, http.StatusOK, map[string]any{"data": out})
}

func (f *fakeLiteLLM) modelNew(w http.ResponseWriter, r *http.Request) {
	var d litellm.Deployment
	gwDecode(r, &d)
	if strings.HasSuffix(d.Params.Model, gatewayRefused) {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "unknown model"})
		return
	}
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
	gwReply(w, http.StatusOK, d)
}

func (f *fakeLiteLLM) modelDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	gwDecode(r, &in)
	n := len(f.deps)
	f.deps = slices.DeleteFunc(f.deps, func(d litellm.Deployment) bool { return d.ID() == in.ID })
	if len(f.deps) == n {
		gwReply(w, http.StatusNotFound, map[string]any{"detail": "no model"})
		return
	}
	gwReply(w, http.StatusOK, map[string]any{"message": "deleted"})
}

func (f *fakeLiteLLM) routerSettings(w http.ResponseWriter, _ *http.Request) {
	gwReply(w, http.StatusOK, map[string]any{"fields": []any{}, "current_values": f.router})
}

func (f *fakeLiteLLM) configUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RouterSettings map[string]any `json:"router_settings"`
	}
	gwDecode(r, &in)
	maps.Copy(f.router, in.RouterSettings)
	gwReply(w, http.StatusOK, map[string]any{"message": "updated"})
}

func (f *fakeLiteLLM) keyList(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, k := range f.keys {
		if k["key_alias"] == r.URL.Query().Get("key_alias") {
			out = append(out, k)
		}
	}
	gwReply(w, http.StatusOK, map[string]any{"keys": out})
}

func (f *fakeLiteLLM) keyUpdate(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	gwDecode(r, &in)
	for _, k := range f.keys {
		if k["token"] == in["key"] {
			for field, v := range in {
				if field != "key" {
					k[field] = v
				}
			}
			gwReply(w, http.StatusOK, k)
			return
		}
	}
	gwReply(w, http.StatusNotFound, map[string]any{"detail": "no key"})
}

func (f *fakeLiteLLM) budgetInfo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Budgets []string `json:"budgets"`
	}
	gwDecode(r, &in)
	out := []map[string]any{}
	for _, id := range in.Budgets {
		if b, ok := f.budget[id]; ok {
			out = append(out, b)
		}
	}
	gwReply(w, http.StatusOK, out)
}

func (f *fakeLiteLLM) budgetSave(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	gwDecode(r, &in)
	id, ok := in["budget_id"].(string)
	if !ok {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "budget_id"})
		return
	}
	if _, exists := f.budget[id]; exists == (r.URL.Path == "/budget/new") {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "budget exists or is missing"})
		return
	}
	f.budget[id] = in
	gwReply(w, http.StatusOK, in)
}

func (f *fakeLiteLLM) daily(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("start_date") == "" || r.URL.Query().Get("end_date") == "" {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "dates"})
		return
	}
	gwReply(w, http.StatusOK, map[string]any{"results": f.days, "metadata": map[string]any{}})
}

func (f *fakeLiteLLM) testConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Params litellm.Params `json:"litellm_params"`
	}
	gwDecode(r, &in)
	if c, ok := f.creds[in.Params.Credential]; ok && c.Values["api_key"] != "bad" {
		gwReply(w, http.StatusOK, map[string]any{"status": "success", "result": map[string]any{}})
		return
	}
	gwReply(w, http.StatusOK, map[string]any{"status": "error", "error": "AuthenticationError: bad key"})
}

// customerSave sets an end user's budget; /customer/update of one LiteLLM
// has not seen is 400, as LiteLLM answers.
func (f *fakeLiteLLM) customerSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID   string `json:"user_id"`
		BudgetID string `json:"budget_id"`
	}
	gwDecode(r, &in)
	if _, ok := f.customers[in.UserID]; ok == (r.URL.Path == "/customer/new") {
		gwReply(w, http.StatusBadRequest, map[string]any{"detail": "end user exists or is missing"})
		return
	}
	f.customers[in.UserID] = in.BudgetID
	gwReply(w, http.StatusOK, map[string]any{"user_id": in.UserID})
}
