package domain

import (
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
	AppBifrost App = "bifrost"
	AppS3      App = "s3"
	AppArgoCD  App = "argocd"
	AppDB      App = "db"
)

// DashboardFloor is an ops dashboard's name and the lowest role that opens
// it, a row of console_dashboards, which an admin may change.
type DashboardFloor struct {
	App   App    `json:"app"`
	Name  string `json:"name"`
	Floor Role   `json:"floor"`
}

// Floor is the lowest role that opens a by default, the floor migration
// 0057 seeds; ok is false for an unknown app. Grafana only shows the
// server's traces, metrics and logs; the others can cancel invocations,
// spend model budget, read every object, deploy or change any row (db,
// pgweb on the stack's database).
func (a App) Floor() (floor Role, ok bool) {
	switch a {
	case AppGrafana:
		return RoleModerator, true
	case AppRestate, AppBifrost, AppS3, AppArgoCD, AppDB:
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
