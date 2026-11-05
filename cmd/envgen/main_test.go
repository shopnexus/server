package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/platform/restatex"
)

// value returns the value of the first key= line in content.
func value(content, key string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnsureEnvCreatesEveryKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, pem := filepath.Join(dir, ".env"), filepath.Join(dir, "identity.pem")

	if err := ensureEnv(env, "", pem); err != nil {
		t.Fatal(err)
	}

	got := read(t, env)
	for _, k := range []string{"JWT_KEY_ID", "JWT_PRIVATE_KEY", "STAFF_KEY", "RESTATE_IDENTITY_KEY"} {
		if value(got, k) == "" {
			t.Errorf("%s is empty in\n%s", k, got)
		}
	}
	public, err := restatex.IdentityKey(pem)
	if err != nil {
		t.Fatal(err)
	}
	if v := value(got, "RESTATE_IDENTITY_KEY"); v != public {
		t.Errorf("RESTATE_IDENTITY_KEY = %q, want the pem's %q", v, public)
	}
	info, err := os.Stat(env)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}

func TestEnsureEnvKeepsExistingKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, pem := filepath.Join(dir, ".env"), filepath.Join(dir, "identity.pem")
	if err := ensureEnv(env, "", pem); err != nil {
		t.Fatal(err)
	}
	before := read(t, env)

	if err := ensureEnv(env, "", pem); err != nil {
		t.Fatal(err)
	}

	if after := read(t, env); after != before {
		t.Errorf("second run changed .env:\n%s\nwant\n%s", after, before)
	}
}

func TestEnsureEnvFillsEmptyLinesAndKeepsOthers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, pem := filepath.Join(dir, ".env"), filepath.Join(dir, "identity.pem")
	if err := os.WriteFile(env, []byte("# a comment\nAPP_ENV=dev\nSTAFF_KEY=\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureEnv(env, "", pem); err != nil {
		t.Fatal(err)
	}

	got := read(t, env)
	if !strings.HasPrefix(got, "# a comment\nAPP_ENV=dev\nSTAFF_KEY=") {
		t.Errorf("the existing lines moved:\n%s", got)
	}
	if n := strings.Count(got, "STAFF_KEY="); n != 1 {
		t.Errorf("STAFF_KEY appears %d times:\n%s", n, got)
	}
	if value(got, "STAFF_KEY") == "" {
		t.Errorf("STAFF_KEY stayed empty:\n%s", got)
	}
	info, err := os.Stat(env)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Errorf("mode = %o, want the file's own 644", mode)
	}
}

func TestEnsureEnvRewritesAStaleRestateKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, pem := filepath.Join(dir, ".env"), filepath.Join(dir, "identity.pem")
	public, err := restatex.EnsureIdentity(pem)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("RESTATE_IDENTITY_KEY=publickeyv1_stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureEnv(env, "", pem); err != nil {
		t.Fatal(err)
	}

	if v := value(read(t, env), "RESTATE_IDENTITY_KEY"); v != public {
		t.Errorf("RESTATE_IDENTITY_KEY = %q, want the pem's %q", v, public)
	}
}

// A new pem would sign Restate's requests with a key the running server does
// not know, so a missing pem next to a public key is an error.
func TestEnsureEnvRefusesAMissingPem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, pem := filepath.Join(dir, ".env"), filepath.Join(dir, "identity.pem")
	if err := os.WriteFile(env, []byte("RESTATE_IDENTITY_KEY=publickeyv1_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ensureEnv(env, "", pem)

	if err == nil || !strings.Contains(err.Error(), "identity.pem") {
		t.Fatalf("err = %v, want one naming identity.pem", err)
	}
	if _, err := os.Stat(pem); !os.IsNotExist(err) {
		t.Errorf("a pem was written: %v", err)
	}
}

func TestEnsureEnvStartsFromTheExample(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, example, pem := filepath.Join(dir, ".env"), filepath.Join(dir, ".env.example"), filepath.Join(dir, "identity.pem")
	if err := os.WriteFile(example, []byte("# the app\nAPP_ENV=dev\nOTEL_EXPORTER_OTLP_ENDPOINT=\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureEnv(env, example, pem); err != nil {
		t.Fatal(err)
	}

	got := read(t, env)
	if !strings.HasPrefix(got, "# the app\nAPP_ENV=dev\nOTEL_EXPORTER_OTLP_ENDPOINT=\n") {
		t.Errorf(".env does not start with the example:\n%s", got)
	}
	if value(got, "JWT_KEY_ID") == "" {
		t.Errorf("no keys after the example:\n%s", got)
	}
}

func TestEnsureEnvAddsVariablesNewInTheExample(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, example, pem := filepath.Join(dir, ".env"), filepath.Join(dir, ".env.example"), filepath.Join(dir, "identity.pem")
	if err := os.WriteFile(example, []byte("# comment\nAPP_ENV=dev\nTRUSTED_PROXIES=\nRESTATE_REGISTER=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("APP_ENV=prod\nTRUSTED_PROXIES=\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureEnv(env, example, pem); err != nil {
		t.Fatal(err)
	}

	got := read(t, env)
	if !strings.HasPrefix(got, "APP_ENV=prod\nTRUSTED_PROXIES=\nRESTATE_REGISTER=true\n") {
		t.Errorf("want the two lines kept and RESTATE_REGISTER added after them:\n%s", got)
	}
	if strings.Contains(got, "# comment") {
		t.Errorf("a comment of the example was copied into an existing .env:\n%s", got)
	}
}
