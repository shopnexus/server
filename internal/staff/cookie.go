package staff

import (
	"net/http"
	"time"
)

const (
	refreshCookie = "staff_refresh"
	// The browser sends the cookie to the refresh and logout endpoints only.
	cookiePath = "/v1/staff/session"
)

// issued is a refresh token and the end of its session, for the cookie.
type issued struct {
	refresh   string
	expiresAt time.Time
}

func setRefreshCookie(w http.ResponseWriter, i issued, now time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: i.refresh, Path: cookiePath, MaxAge: int(i.expiresAt.Sub(now).Seconds()),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: "", Path: cookiePath, MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
