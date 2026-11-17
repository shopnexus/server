package domain

import (
	"net/url"
	"strings"
	"time"
	"uuid"
)

// DashboardTicketTTL is how long a dashboard ticket waits for its callback.
const DashboardTicketTTL = 60 * time.Second

// App is an ops dashboard served behind the console's sign-in, on a host of
// its own (DASHBOARD_HOSTS).
type App string

const (
	AppGrafana App = "grafana"
	AppRestate App = "restate"
	AppLiteLLM App = "litellm"
	AppS3      App = "s3"
	AppArgoCD  App = "argocd"
)

// Floor is the lowest role that opens a; ok is false for an unknown app.
// Grafana only shows the server's traces, metrics and logs; the others can
// cancel invocations, spend model budget, read every object or deploy.
func (a App) Floor() (floor Role, ok bool) {
	switch a {
	case AppGrafana:
		return RoleModerator, true
	case AppRestate, AppLiteLLM, AppS3, AppArgoCD:
		return RoleAdmin, true
	default:
		return "", false
	}
}

// RoleFor is the X-Console-Role the dashboard reads for a member with role r.
// Grafana's auth proxy takes its own role names.
func (a App) RoleFor(r Role) string {
	if a != AppGrafana {
		return string(r)
	}
	if r == RoleAdmin {
		return "Admin"
	}
	return "Viewer"
}

// DashboardTicket lets the console's session open App once before
// ExpiresAt. The row holds the hash of the ticket.
type DashboardTicket struct {
	TokenHash []byte
	SessionID uuid.UUID
	App       App
	CreatedAt time.Time
	ExpiresAt time.Time
}

// DashboardGrant is the member behind a live dashboard session.
type DashboardGrant struct {
	Email string
	Role  Role
}

// StoreCredentials are keys of the object store that last until
// Expiration, from its STS AssumeRole.
type StoreCredentials struct {
	AccessKey    string
	SecretKey    string
	SessionToken string
	Expiration   time.Time
}

// storeConsoleBase is the path of the RustFS console on the s3 host.
const storeConsoleBase = "/rustfs/console"

// StoreConsoleCallback is the RustFS console's SSO callback, which keeps c in
// the browser, where the console signs its S3 calls, and opens rd. The keys
// ride in the fragment, which the browser sends to no server. rd is a path
// on the s3 host; one outside the console, or a sign-in page of it, opens
// the console's start page.
func StoreConsoleCallback(c StoreCredentials, rd string) string {
	page := "/"
	if p, ok := strings.CutPrefix(rd, storeConsoleBase+"/"); ok && !strings.HasPrefix(p, "auth/") {
		page = "/" + p
	}
	f := url.Values{
		"accessKey": {c.AccessKey}, "secretKey": {c.SecretKey}, "sessionToken": {c.SessionToken},
		"expiration": {c.Expiration.UTC().Format(time.RFC3339)}, "redirect": {page},
	}
	return storeConsoleBase + "/auth/oidc-callback/#" + f.Encode()
}
