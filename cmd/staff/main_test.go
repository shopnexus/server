package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Bad arguments stop before the config is read, so this needs no database.
func TestRunRefusesBadArguments(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"usage":             {"no command"},
		"--email":           {"disable"},
		"--name":            {"create-admin", "--email", "a@example.com"},
		"flag provided but": {"enable", "--colour", "red"},
	}
	for want, args := range cases {
		if args[0] == "no command" {
			args = nil
		}
		err := run(context.Background(), args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want a mention of %q", args, err, want)
		}
	}
	if err := run(context.Background(), []string{"promote", "--email", "a@example.com"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("an unknown command: %v", err)
	}
	if err := run(context.Background(), []string{"unlock"}, &bytes.Buffer{}); err == nil || err.Error() != "--email is required" {
		t.Errorf("unlock is a command and needs --email: %v", err)
	}
}
