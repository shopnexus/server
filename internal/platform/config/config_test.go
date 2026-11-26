package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"maps"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testKeys = NewAuthKeys()

var testConsoleKey = NewConsoleKey()

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

// required sets every variable, the ones that may be empty to the empty
// string, for APP_ENV=prod.
func required() map[string]string {
	return map[string]string{
		"APP_ENV":                      "prod",
		"CARRIER_GHTK":                 "off",
		"CARRIER_GHN":                  "off",
		"CARRIER_TOKEN_KEY":            b64(testConsoleKey),
		"CARRIER_POLL_EVERY":           "2h",
		"GHTK_API_URL":                 "",
		"GHTK_PARTNER_TOKEN":           "",
		"GHTK_CLIENT_SOURCE":           "",
		"GHTK_WEBHOOK_SECRET":          "",
		"GHN_API_URL":                  "",
		"GHN_PARTNER_TOKEN":            "",
		"GHN_WEBHOOK_SECRET":           "",
		"HTTP_ADDR":                    ":8080",
		"LOG_LEVEL":                    "info",
		"LOG_FORMAT":                   "json",
		"DATABASE_URL":                 "postgres://db",
		"DATABASE_MAX_CONNS":           "20",
		"DATABASE_STATEMENT_TIMEOUT":   "10s",
		"OTEL_EXPORTER_OTLP_ENDPOINT":  "",
		"SERVICE_NAME":                 "server",
		"TRUSTED_PROXIES":              "",
		"DOCS_ORIGINS":                 "",
		"WEBSITE_ORIGINS":              "",
		"INDEXNOW_KEY":                 "",
		"GEOIP_DB":                     "",
		"RESTATE_INGRESS_URL":          "http://restate:8080",
		"RESTATE_ADMIN_URL":            "http://restate:9070",
		"RESTATE_LISTEN_ADDR":          "127.0.0.1:9080",
		"RESTATE_ENDPOINT_URL":         "",
		"RESTATE_SERVICE_PREFIX":       "",
		"RESTATE_IDENTITY_KEY":         "publickeyv1_test",
		"RESTATE_REGISTER":             "true",
		"JWT_KEY_ID":                   testKeys.JWTKeyID,
		"JWT_PRIVATE_KEY":              b64(testKeys.JWTKey.Seed()),
		"JWT_PREVIOUS_KEY_ID":          "",
		"JWT_PREVIOUS_PUBLIC_KEY":      "",
		"JWT_ISSUER":                   "server",
		"JWT_AUDIENCE":                 "shopnexus-app",
		"CONSOLE_KEY":                  b64(testConsoleKey),
		"CONSOLE_ORIGIN":               "http://console.test",
		"DASHBOARD_HOSTS":              "",
		"OBJECT_STORE_ENDPOINT":        "http://rustfs:9000",
		"OBJECT_STORE_PUBLIC_ENDPOINT": "https://photos.example.test",
		"OBJECT_STORE_REGION":          "us-east-1",
		"OBJECT_STORE_ACCESS_KEY":      "access",
		"OBJECT_STORE_SECRET_KEY":      "secret",
		"OBJECT_STORE_BUCKET":          "server",
		"MEDIA_PUBLIC_URL":             "http://rustfs:9000/server",
		"LLM_BASE_URL":                 "https://llm.example.test/v1",
		"LLM_API_KEY":                  "llm-key",
		"LLM_MODEL":                    "a-model",
		"LLM_REASONING_EFFORT":         "none",
		"EMBED_TEXT_BASE_URL":          "http://openrouter.example.test/v1",
		"EMBED_TEXT_MODEL":             "embed-text",
		"EMBED_TEXT_VERSION":           "bge-m3@5617a9f6",
		"EMBED_IMAGE_VERSION":          "siglip2-so400m-patch16-384-fp32@29f74e67",
		"EMBED_INDEX_API_KEY":          "",
		"EMBED_SEARCH_API_KEY":         "search-key",
		"EMBED_TIMEOUT":                "10s",
		"EMBED_SLOTS":                  "2",
		"EMBED_TEXT_MIN_SIMILARITY":    "0.45",
		"EMBED_PHOTO_MIN_SIMILARITY":   "0.6",
		"EMBED_LOOK_MIN_SIMILARITY":    "0.08",
		"AI_STT_MODELS":                "gipformer-1.5=/models/gipformer-1.5",
		"AI_VAD_MODEL":                 "/models/silero_vad.onnx",
		"AI_NUM_THREADS":               "4",
		"AI_MAX_CONCURRENT":            "2",
		"AI_MAX_AUDIO_SECONDS":         "300",
		"AI_EMBED_MODELS":              "siglip2-so400m-patch16-384=/models/siglip2-so400m-patch16-384",
		"AI_EMBED_MAX_INPUTS":          "16",
		"AI_EMBED_MAX_CONCURRENT":      "2",
		"AI_EMBED_MAX_IMAGE_MB":        "10",
		"AI_FRAMES_MAX_CONCURRENT":     "2",
		"AI_FRAMES_MAX_SECONDS":        "35",
		"AI_FRAMES_MIN_SHARPNESS":      "30",
		"ASSIST_LLM_BASE_URL":          "http://litellm.example.test/v1",
		"ASSIST_LLM_API_KEY":           "assist-key",
		"ASSIST_LLM_MODEL":             "video-read",
		"ASSIST_LLM_REASONING_EFFORT":  "none",
		"ASSIST_CALL_TIMEOUT":          "30s",
		"CHOTOT_GATEWAY_URL":           "https://gateway.chotot.com",
		"CHOTOT_CDN_URL":               "https://cdn.chotot.com",
		"TRUST_LLM_BASE_URL":           "http://litellm.example.test/v1",
		"TRUST_LLM_API_KEY":            "trust-key",
		"TRUST_LLM_MODEL":              "scam-check",
		"TRUST_CALL_TIMEOUT":           "20s",
		"AGENT_BASE_URL":               "http://agent.example.test:3000",
		"AGENT_SERVICE_TOKEN":          "agent-service-token-of-32-characters",
		"AGENT_MCP_ADDR":               "0.0.0.0:9090",
		"AGENT_MCP_TOKEN":              "agent-mcp-token-of-at-least-32-chars",
		"LITELLM_URL":                  "http://litellm.example.test:4000",
		"LITELLM_MASTER_KEY":           "sk-master",
		"FCM_CREDENTIALS":              "",
		"FIREBASE_PROJECT_ID":          "shopnexus-test",
		"FIREBASE_PROJECT_NUMBER":      "123456789",
		"FIREBASE_AUTH_JWKS_URL":       "https://keys.example.test/auth",
		"PNV_JWKS_URL":                 "https://keys.example.test/pnv",
		"PNV_TEST_NUMBERS":             "false",
		"OTP_KEY":                      b64(testKeys.OTPKey),
		"EMAIL_SENDER":                 "resend",
		"RESEND_API_KEY":               "re_test",
		"EMAIL_FROM":                   "ShopNexus <no-reply@shopnexus.test>",
		"GOOGLE_CLIENT_ID":             "web.apps.googleusercontent.com",
		"GOOGLE_JWKS_URL":              "https://www.googleapis.com/oauth2/v3/certs",
	}
}

func TestLoadAllSet(t *testing.T) {
	t.Parallel()
	prev := NewAuthKeys()
	env := required()
	maps.Copy(env, map[string]string{
		"APP_ENV":                     "dev",
		"HTTP_ADDR":                   ":9090",
		"LOG_LEVEL":                   "debug",
		"LOG_FORMAT":                  "text",
		"TRUSTED_PROXIES":             "10.0.0.0/8, 192.168.1.7/32",
		"DOCS_ORIGINS":                "https://docs.example.test, http://localhost:4321",
		"WEBSITE_ORIGINS":             "https://example.test",
		"INDEXNOW_KEY":                "0123456789abcdef0123456789abcdef",
		"GEOIP_DB":                    "/geoip/country.mmdb",
		"JWT_PREVIOUS_KEY_ID":         "old",
		"JWT_PREVIOUS_PUBLIC_KEY":     b64(prev.JWTKey.Public().(ed25519.PublicKey)),
		"JWT_ISSUER":                  "iss",
		"JWT_AUDIENCE":                "aud",
		"RESTATE_LISTEN_ADDR":         "127.0.0.1:9999",
		"RESTATE_ENDPOINT_URL":        "http://host:9999",
		"RESTATE_REGISTER":            "false",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel:4318",
		"SERVICE_NAME":                "svc",
		"PNV_TEST_NUMBERS":            "true",
		"DASHBOARD_HOSTS":             " grafana=Grafana.Dev.Example.test, restate=restate.dev.example.test ,",
	})
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Env:                      "dev",
		Role:                     RoleAll,
		HTTPAddr:                 ":9090",
		LogLevel:                 slog.LevelDebug,
		LogFormat:                "text",
		DatabaseURL:              "postgres://db",
		DatabaseMaxConns:         20,
		DatabaseStatementTimeout: 10 * time.Second,
		Restate: RestateConfig{
			IngressURL: "http://restate:8080", AdminURL: "http://restate:9070",
			ListenAddr: "127.0.0.1:9999", EndpointURL: "http://host:9999", IdentityKey: "publickeyv1_test",
			Register: false,
		},
		OTLPEndpoint: "http://otel:4318",
		ServiceName:  "svc",
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.7/32"),
		},
		DocsOrigins: []string{"https://docs.example.test", "http://localhost:4321"},
		IndexNowKey: "0123456789abcdef0123456789abcdef",
		GeoIPDB:     "/geoip/country.mmdb",
		Auth: AuthConfig{
			OTPKey:       testKeys.OTPKey,
			JWTKeyID:     testKeys.JWTKeyID,
			JWTKey:       testKeys.JWTKey,
			JWTPrevKeyID: "old",
			JWTPrevKey:   prev.JWTKey.Public().(ed25519.PublicKey),
			Issuer:       "iss",
			Audience:     "aud",
		},
		Console: ConsoleConfig{
			Key: testConsoleKey, Origin: "http://console.test",
			DashboardHosts: map[string]string{"grafana": "grafana.dev.example.test", "restate": "restate.dev.example.test"},
		},
		ObjectStore: ObjectStoreConfig{
			Endpoint: "http://rustfs:9000", PublicEndpoint: "https://photos.example.test", Region: "us-east-1",
			AccessKey: "access", SecretKey: "secret", Bucket: "server", PublicURL: "http://rustfs:9000/server",
			UploadOrigins: []string{"https://example.test"},
		},
		LLM: LLMConfig{BaseURL: "https://llm.example.test/v1", APIKey: "llm-key", Model: "a-model", Effort: "none"},
		Embed: EmbedConfig{
			TextBaseURL: "http://openrouter.example.test/v1", TextModel: "embed-text",
			TextVersion: "bge-m3@5617a9f6", ImageVersion: "siglip2-so400m-patch16-384-fp32@29f74e67",
			IndexAPIKey: "", SearchAPIKey: "search-key", Timeout: 10 * time.Second, Slots: 2,
			TextMinSimilarity: 0.45, PhotoMinSimilarity: 0.6, LookMinSimilarity: 0.08,
		},
		AI: AIConfig{
			STTModel: "gipformer-1.5", STTDir: "/models/gipformer-1.5", VADModel: "/models/silero_vad.onnx",
			NumThreads: 4, MaxConcurrent: 2, MaxAudioSeconds: 300,
			EmbedModel: "siglip2-so400m-patch16-384", EmbedDir: "/models/siglip2-so400m-patch16-384",
			EmbedMaxInputs: 16, EmbedMaxConcurrent: 2, EmbedMaxImageMB: 10,
			FramesMaxConcurrent: 2, FramesMaxSeconds: 35, FramesMinSharpness: 30,
		},
		Assist: AssistConfig{
			LLMBaseURL: "http://litellm.example.test/v1", LLMAPIKey: "assist-key", LLMModel: "video-read",
			LLMEffort: "none", CallTimeout: 30 * time.Second,
			ChototGatewayURL: "https://gateway.chotot.com", ChototCDNURL: "https://cdn.chotot.com",
		},
		Trust: TrustConfig{
			LLMBaseURL: "http://litellm.example.test/v1", LLMAPIKey: "trust-key", LLMModel: "scam-check",
			CallTimeout: 20 * time.Second,
		},
		Agent: AgentConfig{
			BaseURL: "http://agent.example.test:3000", ServiceToken: "agent-service-token-of-32-characters",
			MCPAddr: "0.0.0.0:9090", MCPToken: "agent-mcp-token-of-at-least-32-chars",
			LiteLLMURL: "http://litellm.example.test:4000", LiteLLMMasterKey: "sk-master",
		},
		Firebase: FirebaseConfig{
			ProjectID: "shopnexus-test", ProjectNumber: "123456789",
			AuthJWKSURL: "https://keys.example.test/auth", PNVJWKSURL: "https://keys.example.test/pnv", TestNumbers: true,
		},
		Email:   EmailConfig{Sender: "resend", ResendAPIKey: "re_test", From: "ShopNexus <no-reply@shopnexus.test>"},
		Google:  GoogleConfig{ClientID: "web.apps.googleusercontent.com", JWKSURL: "https://www.googleapis.com/oauth2/v3/certs"},
		Carrier: CarrierConfig{GHTKMode: "off", GHNMode: "off", TokenKey: testConsoleKey, PollEvery: 2 * time.Hour},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadAcceptsExplicitEmpty(t *testing.T) {
	t.Parallel()
	got, err := Load(lookup(required()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.OTLPEndpoint != "" || got.TrustedProxies != nil || got.DocsOrigins != nil || got.ObjectStore.UploadOrigins != nil || got.Restate.EndpointURL != "" ||
		got.Restate.ServicePrefix != "" || got.Auth.JWTPrevKeyID != "" || got.Auth.JWTPrevKey != nil ||
		got.Console.DashboardHosts != nil {
		t.Fatalf("empty variables: %+v", got)
	}
}

// No variable has a default: leaving any one out names it.
func TestLoadNamesEveryUnsetVariable(t *testing.T) {
	t.Parallel()
	_, err := Load(lookup(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for name := range required() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	for name := range required() {
		env := required()
		delete(env, name)
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), name+" is not set") {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

func TestLoadRejectsEmptyRequired(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"HTTP_ADDR", "DATABASE_URL", "SERVICE_NAME", "RESTATE_LISTEN_ADDR", "JWT_ISSUER", "JWT_AUDIENCE", "CONSOLE_ORIGIN", "FIREBASE_PROJECT_ID", "FIREBASE_PROJECT_NUMBER", "FIREBASE_AUTH_JWKS_URL", "PNV_JWKS_URL"} {
		env := required()
		env[name] = ""
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), name+" is empty") {
			t.Errorf("empty %s: %v", name, err)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key, value string
		mentions   []string
	}{
		{"LOG_LEVEL", "loud", []string{"LOG_LEVEL", "debug", "info", "warn", "error"}},
		{"PNV_TEST_NUMBERS", "yes", []string{"PNV_TEST_NUMBERS", "true", "false"}},
		{"LOG_FORMAT", "xml", []string{"LOG_FORMAT"}},
		{"APP_ENV", "staging", []string{"APP_ENV"}},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			t.Parallel()
			env := required()
			env[c.key] = c.value
			_, err := Load(lookup(env))
			if err == nil {
				t.Fatal("want error")
			}
			for _, m := range c.mentions {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not mention %q", err, m)
				}
			}
		})
	}
}

func TestLoadRejectsBadKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		set     map[string]string
		mention string
	}{
		{"short otp key", map[string]string{"OTP_KEY": b64(make([]byte, 16))}, "OTP_KEY"},
		{"otp key not base64", map[string]string{"OTP_KEY": "%%%"}, "OTP_KEY"},
		{"seed of the wrong size", map[string]string{"JWT_PRIVATE_KEY": b64(make([]byte, 31))}, "JWT_PRIVATE_KEY"},
		{"previous id without key", map[string]string{"JWT_PREVIOUS_KEY_ID": "old"}, "JWT_PREVIOUS_PUBLIC_KEY"},
		{"previous key without id", map[string]string{"JWT_PREVIOUS_PUBLIC_KEY": b64(make([]byte, 32))}, "JWT_PREVIOUS_KEY_ID"},
		{"previous id equals current", map[string]string{
			"JWT_PREVIOUS_KEY_ID": testKeys.JWTKeyID, "JWT_PREVIOUS_PUBLIC_KEY": b64(make([]byte, 32)),
		}, "must differ"},
		{"bad proxy", map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8,nope"}, "TRUSTED_PROXIES"},
		{"docs origin with a path", map[string]string{"DOCS_ORIGINS": "https://docs.example.test/docs"}, "DOCS_ORIGINS"},
		{"docs origin without a scheme", map[string]string{"DOCS_ORIGINS": "docs.example.test"}, "DOCS_ORIGINS"},
		{"website origin with a path", map[string]string{"WEBSITE_ORIGINS": "https://example.test/tin"}, "WEBSITE_ORIGINS"},
		{"indexnow key too short", map[string]string{"INDEXNOW_KEY": "abc"}, "INDEXNOW_KEY"},
		{"indexnow key with a slash", map[string]string{"INDEXNOW_KEY": "0123456789abcdef/"}, "INDEXNOW_KEY"},
		{"short console key", map[string]string{"CONSOLE_KEY": b64(make([]byte, 16))}, "CONSOLE_KEY"},
		{"short agent service token", map[string]string{"AGENT_SERVICE_TOKEN": "short"}, "AGENT_SERVICE_TOKEN"},
		{"short agent MCP token", map[string]string{"AGENT_MCP_TOKEN": "short"}, "AGENT_MCP_TOKEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := required()
			maps.Copy(env, c.set)
			if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), c.mention) {
				t.Fatalf("err %v, want a mention of %q", err, c.mention)
			}
		})
	}
}

// Production refuses unsigned Restate requests, so it needs the key.
func TestLoadProdNeedsTheRestateIdentityKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"", "not-a-key"} {
		env := required()
		env["RESTATE_IDENTITY_KEY"] = key
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "RESTATE_IDENTITY_KEY") {
			t.Fatalf("%q: %v", key, err)
		}
	}
	env := required()
	env["APP_ENV"], env["RESTATE_IDENTITY_KEY"] = "dev", ""
	if _, err := Load(lookup(env)); err != nil {
		t.Fatalf("dev without the key: %v", err)
	}
}

// Empty push credentials mean the log sender; the project is Firebase's.
func TestLoadPush(t *testing.T) {
	t.Parallel()
	env := required()
	env["FCM_CREDENTIALS"] = b64([]byte(`{"type":"service_account"}`))
	got, err := Load(lookup(env))
	if err != nil || string(got.Push.Credentials) != `{"type":"service_account"}` || got.Firebase.ProjectID != "shopnexus-test" {
		t.Fatalf("%+v %v", got.Push, err)
	}
	env["FCM_CREDENTIALS"] = "%%%"
	if _, err = Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "FCM_CREDENTIALS") {
		t.Fatalf("not base64: %v", err)
	}
}

// RESTATE_REGISTER takes true or false and nothing else.
func TestLoadRestateRegister(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"true": true, "false": false} {
		env := required()
		env["RESTATE_REGISTER"] = value
		got, err := Load(lookup(env))
		if err != nil {
			t.Fatalf("RESTATE_REGISTER=%s: %v", value, err)
		}
		if got.Restate.Register != want {
			t.Errorf("RESTATE_REGISTER=%s: Register = %v", value, got.Restate.Register)
		}
	}
	env := required()
	env["RESTATE_REGISTER"] = "yes"
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), `RESTATE_REGISTER="yes"`) {
		t.Fatalf("RESTATE_REGISTER=yes: %v", err)
	}
}

// DASHBOARD_HOSTS pairs a known dashboard with one host name, each named
// once.
func TestLoadRejectsBadDashboardHosts(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"grafana",
		"=grafana.example.test",
		"kibana=kibana.example.test",
		"grafana=",
		"grafana=https://grafana.example.test",
		"grafana=grafana.example.test:443",
		"grafana=grafana.example.test/x",
		"grafana=-grafana.example.test",
		"grafana=grafana..example.test",
		"grafana=a.example.test,grafana=b.example.test",
		"grafana=same.example.test,restate=same.example.test",
	} {
		env := required()
		env["DASHBOARD_HOSTS"] = value
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "DASHBOARD_HOSTS") {
			t.Errorf("%q: %v", value, err)
		}
	}
	env := required()
	env["DASHBOARD_HOSTS"] = "grafana=grafana.localhost,restate=restate.localhost,litellm=litellm.localhost,s3=s3.localhost,argocd=argocd.localhost,db=db.localhost"
	got, err := Load(lookup(env))
	if err != nil || len(got.Console.DashboardHosts) != len(DashboardApps) {
		t.Fatalf("every dashboard: %v %v", got.Console.DashboardHosts, err)
	}
}

func TestEmbedValuesAreChecked(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ key, value, want string }{
		"timeout":   {"EMBED_TIMEOUT", "soon", "EMBED_TIMEOUT"},
		"slots":     {"EMBED_SLOTS", "0", "EMBED_SLOTS"},
		"pool":      {"DATABASE_MAX_CONNS", "0", "DATABASE_MAX_CONNS"},
		"statement": {"DATABASE_STATEMENT_TIMEOUT", "0s", "DATABASE_STATEMENT_TIMEOUT"},
		"floor":     {"EMBED_TEXT_MIN_SIMILARITY", "1.5", "EMBED_TEXT_MIN_SIMILARITY"},
		"no floor":  {"EMBED_PHOTO_MIN_SIMILARITY", "", "EMBED_PHOTO_MIN_SIMILARITY"},
		"no search": {"EMBED_SEARCH_API_KEY", "\x00unset", "EMBED_SEARCH_API_KEY is not set"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := required()
			if tc.value == "\x00unset" {
				delete(env, tc.key)
			} else {
				env[tc.key] = tc.value
			}
			_, err := Load(lookup(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

func TestLoadEmailSender(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		set     map[string]string
		mention string
	}{
		"unknown sender":       {map[string]string{"EMAIL_SENDER": "smtp"}, "EMAIL_SENDER"},
		"resend without a key": {map[string]string{"RESEND_API_KEY": ""}, "RESEND_API_KEY"},
		"no from":              {map[string]string{"EMAIL_FROM": ""}, "EMAIL_FROM"},
	} {
		env := required()
		maps.Copy(env, c.set)
		_, err := Load(lookup(env))
		if err == nil || !strings.Contains(err.Error(), c.mention) {
			t.Errorf("%s: %v", name, err)
		}
	}
	env := required()
	env["EMAIL_SENDER"], env["RESEND_API_KEY"] = "log", ""
	if cfg, err := Load(lookup(env)); err != nil || cfg.Email.Sender != "log" {
		t.Fatalf("log sender without a key: %v", err)
	}
}

func TestLoadCarrier(t *testing.T) {
	t.Parallel()
	env := required()
	env["CARRIER_GHTK"] = "none"
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "CARRIER_GHTK") {
		t.Fatalf("a switch that is not live, fake or off: %v", err)
	}
	delete(env, "CARRIER_GHN")
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "CARRIER_GHN is not set") {
		t.Fatalf("no CARRIER_GHN: %v", err)
	}
	env = required()
	env["CARRIER_GHN"] = "fake"
	cfg, err := Load(lookup(env))
	if err != nil || cfg.Carrier.GHTKMode != "off" || cfg.Carrier.GHNMode != "fake" || len(cfg.Carrier.TokenKey) != 32 || cfg.Carrier.PollEvery != 2*time.Hour {
		t.Fatalf("GHN fake, GHTK off: %+v %v", cfg.Carrier, err)
	}
}

// A carrier live takes all its own settings and none of the other's; fake
// and off take none, so a stack cannot half-point at a real carrier.
func TestLoadCarrierLive(t *testing.T) {
	t.Parallel()
	ghtk := map[string]string{
		"GHTK_API_URL": "https://services-staging.ghtklab.com", "GHTK_PARTNER_TOKEN": "partner", "GHTK_CLIENT_SOURCE": "S1", "GHTK_WEBHOOK_SECRET": "w1",
	}
	ghn := map[string]string{
		"GHN_API_URL": "https://dev-online-gateway.ghn.vn/shiip/public-api", "GHN_PARTNER_TOKEN": "ghn-partner", "GHN_WEBHOOK_SECRET": "w2",
	}
	env := required()
	env["CARRIER_GHN"] = "live"
	maps.Copy(env, ghn)
	cfg, err := Load(lookup(env))
	if err != nil || cfg.Carrier.GHNMode != "live" || cfg.Carrier.GHTKMode != "off" || cfg.Carrier.GHNURL != ghn["GHN_API_URL"] || cfg.Carrier.GHNPartner != "ghn-partner" {
		t.Fatalf("GHN live alone: %+v %v", cfg.Carrier, err)
	}
	env["CARRIER_GHTK"] = "live"
	maps.Copy(env, ghtk)
	if cfg, err = Load(lookup(env)); err != nil || cfg.Carrier.GHTKPartner != "partner" || cfg.Carrier.GHTKSource != "S1" {
		t.Fatalf("both live: %+v %v", cfg.Carrier, err)
	}
	for sw, live := range map[string]map[string]string{"CARRIER_GHTK": ghtk, "CARRIER_GHN": ghn} {
		for key := range live {
			missing := required()
			missing[sw] = "live"
			maps.Copy(missing, live)
			missing[key] = ""
			if _, err = Load(lookup(missing)); err == nil || !strings.Contains(err.Error(), key+" is required with "+sw+"=live") {
				t.Errorf("live without %s: %v", key, err)
			}
			for _, mode := range []string{"fake", "off"} {
				other := required()
				other[sw] = mode
				other[key] = live[key]
				if _, err = Load(lookup(other)); err == nil || !strings.Contains(err.Error(), key+" is set only with "+sw+"=live") {
					t.Errorf("%s with %s: %v", mode, key, err)
				}
			}
		}
	}
	short := required()
	short["CARRIER_TOKEN_KEY"] = b64(make([]byte, 16))
	if _, err = Load(lookup(short)); err == nil || !strings.Contains(err.Error(), "CARRIER_TOKEN_KEY") {
		t.Fatalf("a short token key: %v", err)
	}
}

// An empty model turns its kind of vector off; the kind's other settings may
// then be empty, and stay needed while the model is set.
func TestLoadEmbedKindsOff(t *testing.T) {
	t.Parallel()
	env := required()
	for _, k := range []string{
		"EMBED_TEXT_MODEL", "EMBED_TEXT_BASE_URL", "EMBED_TEXT_VERSION", "EMBED_TEXT_MIN_SIMILARITY",
		"AI_EMBED_MODELS", "EMBED_IMAGE_VERSION", "EMBED_PHOTO_MIN_SIMILARITY", "EMBED_LOOK_MIN_SIMILARITY",
	} {
		env[k] = ""
	}
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Embed.TextOn() || got.AI.EmbedOn() || got.AI.EmbedDir != "" || got.Embed.TextVersion != "" || got.Embed.PhotoMinSimilarity != 0 {
		t.Fatalf("embed = %+v, want both kinds off", got.Embed)
	}
	env = required()
	env["EMBED_IMAGE_VERSION"] = ""
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "EMBED_IMAGE_VERSION is empty") {
		t.Fatalf("image on without a version: %v", err)
	}
	env = required()
	delete(env, "EMBED_TEXT_BASE_URL")
	env["EMBED_TEXT_MODEL"] = ""
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "EMBED_TEXT_BASE_URL is not set") {
		t.Fatalf("text off without its base URL set: %v", err)
	}
}

// SERVER_ROLE is the one setting with a default: unset or empty is all.
func TestLoadRole(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]Role{"\x00unset": RoleAll, "": RoleAll, "all": RoleAll, "api": RoleAPI, "ai": RoleAI} {
		env := required()
		if v != "\x00unset" {
			env["SERVER_ROLE"] = v
		}
		got, err := Load(lookup(env))
		if err != nil || got.Role != want {
			t.Errorf("SERVER_ROLE=%q: %q %v", v, got.Role, err)
		}
	}
	env := required()
	env["SERVER_ROLE"] = "models"
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "SERVER_ROLE") {
		t.Fatalf("an unknown role: %v", err)
	}
	if !RoleAll.ServesAPI() || !RoleAll.ServesAI() || RoleAPI.ServesAI() || RoleAI.ServesAPI() ||
		!Role("").ServesAPI() || !Role("").ServesAI() {
		t.Fatal("ServesAPI or ServesAI")
	}
}

func TestLoadAIValuesAreChecked(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ key, value, want string }{
		"no stt model":      {"AI_STT_MODELS", "", "AI_STT_MODELS is empty"},
		"stt not name=dir":  {"AI_STT_MODELS", "gipformer-1.5", "AI_STT_MODELS"},
		"two stt models":    {"AI_STT_MODELS", "a=/m/a,b=/m/b", "AI_STT_MODELS"},
		"two embed models":  {"AI_EMBED_MODELS", "a=/m/a,b=/m/b", "AI_EMBED_MODELS"},
		"embed model unset": {"AI_EMBED_MODELS", "\x00unset", "AI_EMBED_MODELS is not set"},
		"no vad":            {"AI_VAD_MODEL", "", "AI_VAD_MODEL is empty"},
		"threads":           {"AI_NUM_THREADS", "0", "AI_NUM_THREADS"},
		"inputs":            {"AI_EMBED_MAX_INPUTS", "many", "AI_EMBED_MAX_INPUTS"},
		"sharpness":         {"AI_FRAMES_MIN_SHARPNESS", "-1", "AI_FRAMES_MIN_SHARPNESS"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := required()
			if tc.value == "\x00unset" {
				delete(env, tc.key)
			} else {
				env[tc.key] = tc.value
			}
			_, err := Load(lookup(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %s", err, tc.want)
			}
		})
	}
	env := required()
	env["AI_FRAMES_MIN_SHARPNESS"] = "0"
	if got, err := Load(lookup(env)); err != nil || got.AI.FramesMinSharpness != 0 {
		t.Fatalf("a sharpness floor of zero: %v", err)
	}
}
