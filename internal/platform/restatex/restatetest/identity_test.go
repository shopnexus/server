package restatetest

import "testing"

func TestIdentityFileFromTheEnvironment(t *testing.T) {
	t.Setenv("TEST_RESTATE_IDENTITY_FILE", "/keys/identity.pem")

	if got := identityFile(); got != "/keys/identity.pem" {
		t.Fatalf("identityFile() = %q, want /keys/identity.pem", got)
	}
}
