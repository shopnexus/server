// Command envgen fills in a server .env file:
//
//	envgen -env .env -restate .restate/identity.pem [-example .env.example]
//
// With -example, a missing .env starts as a copy of the example, and an
// existing one gets every variable the example has and it lacks, with the
// example's value. Then it adds the keys: JWT_KEY_ID and JWT_PRIVATE_KEY,
// STAFF_KEY, and RESTATE_IDENTITY_KEY, the public half of the Restate request
// identity key at -restate, which it creates when missing. A variable already
// set stays, so a second run changes nothing. make run, make test,
// infra/cluster/seal.sh and the root docker-compose.yml call it.
package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

func main() {
	envPath := flag.String("env", "", "the .env file to fill in")
	examplePath := flag.String("example", "", "the .env.example to copy missing variables from")
	pemPath := flag.String("restate", "", "path of the Restate identity key, created when missing")
	flag.Parse()
	err := errors.New("-env and -restate are required")
	if *envPath != "" && *pemPath != "" {
		err = ensureEnv(*envPath, *examplePath, *pemPath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "envgen:", err)
		os.Exit(1)
	}
}

// ensureEnv fills in the .env file at envPath from the example at
// examplePath, when it is not empty, and adds the keys missing from it or set
// to the empty string there. A new file gets mode 0600; an existing one keeps
// its mode, its other lines and their order.
func ensureEnv(envPath, examplePath, pemPath string) error {
	data, err := os.ReadFile(envPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", envPath, err)
	}
	var example []byte
	if examplePath != "" {
		if example, err = os.ReadFile(examplePath); err != nil {
			return fmt.Errorf("read %s: %w", examplePath, err)
		}
	}
	split := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}

	lines := split(data)
	if len(data) == 0 {
		lines = split(example)
	}
	find := func(key string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, key+"=") {
				return i
			}
		}
		return -1
	}
	get := func(key string) string {
		if i := find(key); i >= 0 {
			return strings.TrimPrefix(lines[i], key+"=")
		}
		return ""
	}
	set := func(key, value string) {
		if i := find(key); i >= 0 {
			lines[i] = key + "=" + value
			return
		}
		lines = append(lines, key+"="+value)
	}

	for _, l := range split(example) {
		key, _, ok := strings.Cut(l, "=")
		if ok && !strings.HasPrefix(l, "#") && find(key) < 0 {
			lines = append(lines, l)
		}
	}

	if get("JWT_KEY_ID") == "" || get("JWT_PRIVATE_KEY") == "" {
		for l := range strings.SplitSeq(strings.TrimSpace(config.NewAuthKeys().Env()), "\n") {
			k, v, _ := strings.Cut(l, "=")
			set(k, v)
		}
	}
	if get("STAFF_KEY") == "" {
		set("STAFF_KEY", base64.StdEncoding.EncodeToString(config.NewStaffKey()))
	}

	// A new pem next to a public key in .env would sign Restate's requests
	// with a key the running server does not know.
	if _, statErr := os.Stat(pemPath); errors.Is(statErr, fs.ErrNotExist) && get("RESTATE_IDENTITY_KEY") != "" {
		return fmt.Errorf("%s has RESTATE_IDENTITY_KEY but %s is missing: restore it from the backup, or delete the line to make a new key", envPath, pemPath)
	}
	public, err := restatex.EnsureIdentity(pemPath)
	if err != nil {
		return fmt.Errorf("restate identity key: %w", err)
	}
	set("RESTATE_IDENTITY_KEY", public)

	out := strings.Join(lines, "\n") + "\n"
	if out == string(data) {
		return nil
	}
	// WriteFile applies the mode only when it creates the file.
	if err := os.WriteFile(envPath, []byte(out), 0o600); err != nil { //nolint:gosec // The path is the operator's own -env flag.
		return fmt.Errorf("write %s: %w", envPath, err)
	}
	return nil
}
