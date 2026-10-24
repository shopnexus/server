package staff

import (
	"net/http"
	"time"
)

const (
	sessionCookie = "staff_session"
	cookiePath    = "/v1/staff"
)

// setSessionCookie keeps the session token where the console's scripts
// cannot read it. SameSite=Strict keeps other sites' requests from carrying
// it, and FromConsole refuses any staff call without the console's header.
func setSessionCookie(w http.ResponseWriter, tok string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: cookiePath, MaxAge: int(ttl.Seconds()),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: cookiePath, MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
