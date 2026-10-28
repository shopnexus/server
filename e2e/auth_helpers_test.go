package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// seq numbers the phones and addresses tests make up, so no two tests share
// one. The database is new each run.
var seq atomic.Int64

// newPhone returns a fresh mobile number.
func newPhone() string { return fmt.Sprintf("+8499%07d", seq.Add(1)) }

type signIn struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Account      struct {
		ID    string `json:"id"`
		IsNew bool   `json:"is_new"`
	} `json:"account"`
}

// suspend marks an account suspended, as an admin would.
func suspend(t *testing.T, accountID string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`UPDATE accounts SET status = 'suspended', suspend_reason = 'e2e' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": accountID}); err != nil {
		t.Fatal(err)
	}
}

// signInCall signs in with a phone token and a fresh key unless headers
// carry one.
func signInCall(t *testing.T, token string, headers ...[2]string) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/auth/sign-in",
		map[string]string{"phone_token": token, "device_name": "e2e"}, append([][2]string{newKey()}, headers...)...)
}

// signInStatus signs in and returns the status without touching t, so
// goroutines can call it. It records the status for the coverage check as
// call does and skips response validation, so its caller checks statuses.
func signInStatus(token string) int {
	body, _ := json.Marshal(map[string]string{"phone_token": token})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server+"/v1/auth/sign-in", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewV7().String())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	res.Body.Close()
	record("signIn", res.StatusCode)
	return res.StatusCode
}

func signInWith(t *testing.T, phone string) signIn {
	t.Helper()
	res := signInCall(t, issuer.SMSToken(phone))
	if res.Status != http.StatusOK {
		t.Fatalf("sign in: %d %s", res.Status, res.Body)
	}
	return decode[signIn](t, res)
}

func newAccount(t *testing.T) (account signIn, phone string) {
	t.Helper()
	phone = newPhone()
	return signInWith(t, phone), phone
}
