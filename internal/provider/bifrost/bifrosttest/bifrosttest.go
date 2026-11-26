// Package bifrosttest is Bifrost for tests, in memory: the admin API the
// server uses (teams, virtual keys, pricing overrides, providers, the
// model catalog and the logs) behind Basic auth, and inference, which
// checks the virtual key and its allowed models as Bifrost does and hands
// the call to the upstream a test set for the key's caller.
package bifrosttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/provider/bifrost"
)

// The admin user the fake takes.
const (
	Username = "admin"
	Password = "bifrost-test-password"
)

// Call is an inference call the fake took, as the upstream got it.
type Call struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

type team struct {
	ID      string
	Name    string
	Budget  *float64
	Usage   float64
	Reset   time.Time
	Aligned bool
}

type usage struct {
	At    time.Time
	Team  string // a team's name
	USD   float64
	Count int
}

// Fake is Bifrost in memory. Its fields are set through its methods.
type Fake struct {
	srv       *httptest.Server
	mu        sync.Mutex
	teams     []*team
	keys      []*bifrost.VirtualKey
	overrides []map[string]any
	providers []bifrost.Provider
	catalog   map[string][]bifrost.Model
	stats     map[string]bifrost.Stats
	usages    []usage
	upstream  map[string]string
	calls     map[string][]Call
	down      bool
	writes    int
}

// New starts the fake; Close stops it.
func New() *Fake {
	f := &Fake{
		catalog: map[string][]bifrost.Model{}, stats: map[string]bifrost.Stats{}, upstream: map[string]string{},
		calls: map[string][]Call{},
	}
	admin := http.NewServeMux()
	admin.HandleFunc("GET /api/governance/teams", f.listTeams)
	admin.HandleFunc("POST /api/governance/teams", f.createTeam)
	admin.HandleFunc("PUT /api/governance/teams/{id}", f.updateTeam)
	admin.HandleFunc("DELETE /api/governance/teams/{id}", f.deleteTeam)
	admin.HandleFunc("GET /api/governance/virtual-keys", f.listKeys)
	admin.HandleFunc("POST /api/governance/virtual-keys", f.createKey)
	admin.HandleFunc("PUT /api/governance/virtual-keys/{id}", f.updateKey)
	admin.HandleFunc("DELETE /api/governance/virtual-keys/{id}", f.deleteKey)
	admin.HandleFunc("GET /api/governance/pricing-overrides", f.listOverrides)
	admin.HandleFunc("POST /api/governance/pricing-overrides", f.createOverride)
	admin.HandleFunc("PUT /api/governance/pricing-overrides/{id}", f.updateOverride)
	admin.HandleFunc("DELETE /api/governance/pricing-overrides/{id}", f.deleteOverride)
	admin.HandleFunc("GET /api/providers", f.listProviders)
	admin.HandleFunc("GET /api/models/details", f.models)
	admin.HandleFunc("GET /api/logs/histogram/cost/by-dimension", f.costs)
	admin.HandleFunc("GET /api/logs/histogram", f.counts)
	admin.HandleFunc("GET /api/logs/stats", f.logStats)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		if down {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			f.infer(w, r)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != Username || pass != Password {
			reply(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "Unauthorized"}})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodGet {
			f.writes++
		}
		admin.ServeHTTP(w, r)
	}))
	return f
}

// Close stops the fake.
func (f *Fake) Close() { f.srv.Close() }

// URL is the fake's base URL, without /v1.
func (f *Fake) URL() string { return f.srv.URL }

// Admin is a client of the fake's admin API.
func (f *Fake) Admin() bifrost.Admin {
	return bifrost.Admin{BaseURL: f.srv.URL, Username: Username, Password: Password, HTTP: f.srv.Client()}
}

// SetDown makes every call answer 502 while down.
func (f *Fake) SetDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// Writes counts the admin calls that changed something.
func (f *Fake) Writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// SetProviders sets the providers of config.json.
func (f *Fake) SetProviders(list ...bifrost.Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers = list
}

// SetCatalog sets a provider's catalog.
func (f *Fake) SetCatalog(provider string, models ...bifrost.Model) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalog[provider] = models
}

// SetStats sets a provider's requests and success rate, in percent, of
// any window.
func (f *Fake) SetStats(provider string, requests int, successRate float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats[provider] = bifrost.Stats{Requests: requests, SuccessRate: successRate}
}

// Log records count requests that cost usd for the team named team at at.
func (f *Fake) Log(at time.Time, team string, usd float64, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usages = append(f.usages, usage{At: at, Team: team, USD: usd, Count: count})
}

// SetSpent sets the spend of a team's budget this month.
func (f *Fake) SetSpent(team string, usd float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.teams {
		if t.Name == team {
			t.Usage = usd
		}
	}
}

// SetUpstream sends the inference calls of caller, a virtual key's name,
// to baseURL (which ends where /chat/completions and /embeddings go).
func (f *Fake) SetUpstream(caller, baseURL string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstream[caller] = baseURL
}

// Calls lists caller's inference calls.
func (f *Fake) Calls(caller string) []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls[caller])
}

// Teams, Keys and Overrides are the objects in the fake, by name.
func (f *Fake) Teams() map[string]bifrost.Team {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]bifrost.Team{}
	for _, t := range f.teams {
		out[t.Name] = teamOut(t)
	}
	return out
}

func (f *Fake) Keys() map[string]bifrost.VirtualKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]bifrost.VirtualKey{}
	for _, k := range f.keys {
		out[k.Name] = *k
	}
	return out
}

func (f *Fake) Overrides() map[string]map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]map[string]any{}
	for _, o := range f.overrides {
		out[o["name"].(string)] = maps.Clone(o)
	}
	return out
}

// PutKey adds a key as another writer would, such as Bifrost's UI.
func (f *Fake) PutKey(k bifrost.VirtualKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k.ID == "" {
		k.ID = uuid.NewV7().String()
	}
	f.keys = append(f.keys, &k)
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func refuse(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]any{"error": map[string]any{"message": msg}})
}

func teamOut(t *team) bifrost.Team {
	out := bifrost.Team{ID: t.ID, Name: t.Name, Budgets: []bifrost.Budget{}, CalendarAligned: t.Aligned}
	if t.Budget != nil {
		out.Budgets = append(out.Budgets, bifrost.Budget{
			MaxLimit: *t.Budget, ResetDuration: "1M", CurrentUsage: t.Usage, LastReset: t.Reset,
		})
	}
	return out
}

func (f *Fake) listTeams(w http.ResponseWriter, _ *http.Request) {
	out := make([]bifrost.Team, 0, len(f.teams))
	for _, t := range f.teams {
		out = append(out, teamOut(t))
	}
	reply(w, http.StatusOK, map[string]any{"teams": out, "count": len(out)})
}

type teamBody struct {
	Name    string           `json:"name"`
	Budgets []bifrost.Budget `json:"budgets"`
	Aligned bool             `json:"calendar_aligned"`
}

func (b teamBody) apply(t *team) bool {
	t.Name, t.Aligned, t.Budget = b.Name, b.Aligned, nil
	for _, bu := range b.Budgets {
		if bu.MaxLimit <= 0 || bu.ResetDuration == "" {
			return false
		}
		t.Budget = new(bu.MaxLimit)
	}
	return len(b.Budgets) <= 1
}

func (f *Fake) createTeam(w http.ResponseWriter, r *http.Request) {
	var b teamBody
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Name == "" {
		refuse(w, http.StatusBadRequest, "bad team")
		return
	}
	if slices.ContainsFunc(f.teams, func(t *team) bool { return t.Name == b.Name }) {
		refuse(w, http.StatusConflict, "team name taken")
		return
	}
	now := time.Now().UTC()
	t := &team{ID: uuid.NewV7().String(), Reset: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)}
	if !b.apply(t) {
		refuse(w, http.StatusBadRequest, "bad budget")
		return
	}
	f.teams = append(f.teams, t)
	reply(w, http.StatusOK, map[string]any{"team": teamOut(t)})
}

func (f *Fake) updateTeam(w http.ResponseWriter, r *http.Request) {
	i := slices.IndexFunc(f.teams, func(t *team) bool { return t.ID == r.PathValue("id") })
	var b teamBody
	if i < 0 || json.NewDecoder(r.Body).Decode(&b) != nil {
		refuse(w, http.StatusNotFound, "no team")
		return
	}
	t := *f.teams[i]
	if !b.apply(&t) {
		refuse(w, http.StatusBadRequest, "bad budget")
		return
	}
	*f.teams[i] = t
	reply(w, http.StatusOK, map[string]any{"team": teamOut(f.teams[i])})
}

func (f *Fake) deleteTeam(w http.ResponseWriter, r *http.Request) {
	f.teams = slices.DeleteFunc(f.teams, func(t *team) bool { return t.ID == r.PathValue("id") })
	reply(w, http.StatusOK, map[string]any{})
}

func (f *Fake) listKeys(w http.ResponseWriter, _ *http.Request) {
	out := make([]bifrost.VirtualKey, 0, len(f.keys))
	for _, k := range f.keys {
		out = append(out, *k)
	}
	reply(w, http.StatusOK, map[string]any{"virtual_keys": out, "count": len(out)})
}

type keyBody struct {
	Name      string  `json:"name"`
	TeamID    *string `json:"team_id"`
	IsActive  *bool   `json:"is_active"`
	Providers []struct {
		Provider      string   `json:"provider"`
		AllowedModels []string `json:"allowed_models"`
		KeyIDs        []string `json:"key_ids"`
	} `json:"provider_configs"`
	RateLimit *bifrost.RateLimit `json:"rate_limit"`
	Metadata  map[string]string  `json:"metadata"`
}

func (f *Fake) apply(k *bifrost.VirtualKey, b keyBody) string {
	if b.TeamID != nil && !slices.ContainsFunc(f.teams, func(t *team) bool { return t.ID == *b.TeamID }) {
		return "no such team"
	}
	for key, v := range b.Metadata {
		if len(v) > 512 || strings.Trim(key, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") != "" {
			return "bad metadata"
		}
	}
	k.Name, k.TeamID, k.IsActive, k.Metadata = b.Name, b.TeamID, b.IsActive, b.Metadata
	k.ProviderConfigs = []bifrost.ProviderConfig{}
	for _, p := range b.Providers {
		k.ProviderConfigs = append(k.ProviderConfigs, bifrost.ProviderConfig{
			Provider: p.Provider, AllowedModels: p.AllowedModels, AllowAllKeys: slices.Contains(p.KeyIDs, "*"),
		})
	}
	switch {
	case b.RateLimit == nil:
	case *b.RateLimit == (bifrost.RateLimit{}):
		k.RateLimit = nil
	default:
		k.RateLimit = b.RateLimit
	}
	return ""
}

func (f *Fake) createKey(w http.ResponseWriter, r *http.Request) {
	var b keyBody
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Name == "" {
		refuse(w, http.StatusBadRequest, "bad key")
		return
	}
	if slices.ContainsFunc(f.keys, func(k *bifrost.VirtualKey) bool { return k.Name == b.Name }) {
		refuse(w, http.StatusConflict, "key name taken")
		return
	}
	k := &bifrost.VirtualKey{ID: uuid.NewV7().String(), Value: "sk-bf-" + uuid.NewV7().String()}
	if msg := f.apply(k, b); msg != "" {
		refuse(w, http.StatusBadRequest, msg)
		return
	}
	f.keys = append(f.keys, k)
	reply(w, http.StatusOK, map[string]any{"virtual_key": k})
}

func (f *Fake) updateKey(w http.ResponseWriter, r *http.Request) {
	i := slices.IndexFunc(f.keys, func(k *bifrost.VirtualKey) bool { return k.ID == r.PathValue("id") })
	var b keyBody
	if i < 0 || json.NewDecoder(r.Body).Decode(&b) != nil {
		refuse(w, http.StatusNotFound, "no key")
		return
	}
	k := *f.keys[i]
	if msg := f.apply(&k, b); msg != "" {
		refuse(w, http.StatusBadRequest, msg)
		return
	}
	*f.keys[i] = k
	reply(w, http.StatusOK, map[string]any{"virtual_key": k})
}

func (f *Fake) deleteKey(w http.ResponseWriter, r *http.Request) {
	f.keys = slices.DeleteFunc(f.keys, func(k *bifrost.VirtualKey) bool { return k.ID == r.PathValue("id") })
	reply(w, http.StatusOK, map[string]any{})
}

func (f *Fake) listOverrides(w http.ResponseWriter, _ *http.Request) {
	out := make([]map[string]any, 0, len(f.overrides))
	for _, o := range f.overrides {
		raw, _ := json.Marshal(o["patch"])
		out = append(out, map[string]any{
			"id": o["id"], "name": o["name"], "provider_id": o["provider_id"], "pattern": o["pattern"],
			"pricing_patch": string(raw),
		})
	}
	reply(w, http.StatusOK, map[string]any{"pricing_overrides": out})
}

func overrideOf(r *http.Request) (map[string]any, bool) {
	var o map[string]any
	if json.NewDecoder(r.Body).Decode(&o) != nil {
		return nil, false
	}
	types, _ := o["request_types"].([]any)
	for _, t := range types {
		if s, _ := t.(string); strings.HasSuffix(s, "_stream") {
			return nil, false
		}
	}
	return o, o["name"] != nil && o["scope_kind"] == "provider" && o["match_type"] == "exact"
}

func (f *Fake) createOverride(w http.ResponseWriter, r *http.Request) {
	o, ok := overrideOf(r)
	if !ok {
		refuse(w, http.StatusBadRequest, "bad override")
		return
	}
	o["id"] = uuid.NewV7().String()
	f.overrides = append(f.overrides, o)
	reply(w, http.StatusOK, map[string]any{"pricing_override": o})
}

func (f *Fake) updateOverride(w http.ResponseWriter, r *http.Request) {
	i := slices.IndexFunc(f.overrides, func(o map[string]any) bool { return o["id"] == r.PathValue("id") })
	o, ok := overrideOf(r)
	if i < 0 || !ok {
		refuse(w, http.StatusBadRequest, "bad override")
		return
	}
	o["id"] = f.overrides[i]["id"]
	f.overrides[i] = o
	reply(w, http.StatusOK, map[string]any{"pricing_override": o})
}

func (f *Fake) deleteOverride(w http.ResponseWriter, r *http.Request) {
	f.overrides = slices.DeleteFunc(f.overrides, func(o map[string]any) bool { return o["id"] == r.PathValue("id") })
	reply(w, http.StatusOK, map[string]any{})
}

func (f *Fake) listProviders(w http.ResponseWriter, _ *http.Request) {
	out := make([]map[string]any, 0, len(f.providers))
	for _, p := range f.providers {
		v := map[string]any{"name": p.Name, "network_config": map[string]any{"base_url": p.BaseURL}, "provider_status": p.Status}
		if p.Custom != "" {
			v["custom_provider_config"] = map[string]any{"base_provider_type": p.Custom}
		}
		out = append(out, v)
	}
	reply(w, http.StatusOK, map[string]any{"providers": out, "total": len(out)})
}

func (f *Fake) models(w http.ResponseWriter, r *http.Request) {
	list := f.catalog[r.URL.Query().Get("provider")]
	n := 20
	_, _ = fmt.Sscan(r.URL.Query().Get("limit"), &n)
	reply(w, http.StatusOK, map[string]any{"models": list[:min(n, len(list))], "total": len(list)})
}

// window reads a logs query's window and its bucket size, as Bifrost
// picks it.
func window(r *http.Request) (from, to time.Time, size time.Duration, ok bool) {
	from, errFrom := time.Parse(time.RFC3339, r.URL.Query().Get("start_time"))
	to, errTo := time.Parse(time.RFC3339, r.URL.Query().Get("end_time"))
	if errFrom != nil || errTo != nil {
		return from, to, 0, false
	}
	day := 24 * time.Hour
	switch d := to.Sub(from); {
	case d >= 365*day:
		size = 30 * day
	case d >= 90*day:
		size = 7 * day
	case d > 31*day:
		size = 3 * day
	case d >= 7*day:
		size = day
	case d >= 3*day:
		size = 8 * time.Hour
	case d >= day:
		size = time.Hour
	default:
		size = time.Minute
	}
	return from, to, size, true
}

func (f *Fake) teamName(id string) string {
	for _, t := range f.teams {
		if t.ID == id {
			return t.Name
		}
	}
	return ""
}

func (f *Fake) teamID(name string) string {
	for _, t := range f.teams {
		if t.Name == name {
			return t.ID
		}
	}
	return "gone-" + name
}

func (f *Fake) costs(w http.ResponseWriter, r *http.Request) {
	from, to, size, ok := window(r)
	if !ok || r.URL.Query().Get("dimension") != "team_id" {
		refuse(w, http.StatusBadRequest, "bad window")
		return
	}
	at := map[int64]*bifrost.CostBucket{}
	for _, u := range f.usages {
		if u.At.Before(from) || !u.At.Before(to) {
			continue
		}
		start := u.At.Truncate(size).Unix()
		b := at[start]
		if b == nil {
			b = &bifrost.CostBucket{At: time.Unix(start, 0).UTC(), ByTeam: map[string]float64{}}
			at[start] = b
		}
		b.Total += u.USD
		b.ByTeam[f.teamID(u.Team)] += u.USD
	}
	out := make([]bifrost.CostBucket, 0, len(at))
	for _, k := range slices.Sorted(maps.Keys(at)) {
		out = append(out, *at[k])
	}
	reply(w, http.StatusOK, map[string]any{"buckets": out, "bucket_size_seconds": int(size.Seconds())})
}

func (f *Fake) counts(w http.ResponseWriter, r *http.Request) {
	from, to, size, ok := window(r)
	if !ok {
		refuse(w, http.StatusBadRequest, "bad window")
		return
	}
	team := f.teamName(r.URL.Query().Get("team_ids"))
	at := map[int64]int{}
	for _, u := range f.usages {
		if u.Team == team && !u.At.Before(from) && u.At.Before(to) {
			at[u.At.Truncate(size).Unix()] += u.Count
		}
	}
	out := make([]bifrost.CountBucket, 0, len(at))
	for _, k := range slices.Sorted(maps.Keys(at)) {
		out = append(out, bifrost.CountBucket{At: time.Unix(k, 0).UTC(), Count: at[k]})
	}
	reply(w, http.StatusOK, map[string]any{"buckets": out, "bucket_size_seconds": int(size.Seconds())})
}

func (f *Fake) logStats(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, f.stats[r.URL.Query().Get("providers")])
}

// infer serves an inference call as Bifrost's governance does: a key it
// lacks is 401, a key turned off or a model the key may not call 403;
// then the call goes to the caller's upstream as it came.
func (f *Fake) infer(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		refuse(w, http.StatusBadRequest, "bad body")
		return
	}
	f.mu.Lock()
	value, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	i := slices.IndexFunc(f.keys, func(k *bifrost.VirtualKey) bool { return k.Value == value })
	var key bifrost.VirtualKey
	if i >= 0 {
		key = *f.keys[i]
	}
	upstream := f.upstream[key.Name]
	if i >= 0 {
		f.calls[key.Name] = append(f.calls[key.Name], Call{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	}
	f.mu.Unlock()
	if i < 0 {
		refuse(w, http.StatusUnauthorized, "virtual key not found")
		return
	}
	model, _ := body["model"].(string)
	provider, name, _ := strings.Cut(model, "/")
	allowed := slices.ContainsFunc(key.ProviderConfigs, func(p bifrost.ProviderConfig) bool {
		return p.Provider == provider && p.AllowAllKeys && slices.Contains(p.AllowedModels, name)
	})
	if !key.Active() || !allowed {
		refuse(w, http.StatusForbidden, "model not allowed for this virtual key")
		return
	}
	if upstream == "" {
		refuse(w, http.StatusNotFound, "no upstream for "+key.Name)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		upstream+strings.TrimPrefix(r.URL.Path, "/v1"), bytes.NewReader(raw))
	if err != nil {
		refuse(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		refuse(w, http.StatusBadGateway, err.Error())
		return
	}
	defer func() { _ = res.Body.Close() }()
	maps.Copy(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
