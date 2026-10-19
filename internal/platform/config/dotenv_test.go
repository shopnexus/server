package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// unset clears a variable for the test and restores it afterwards.
func unset(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	os.Unsetenv(key)
}

func TestLoadDotEnvBasic(t *testing.T) {
	unset(t, "DOTENV_A")
	if err := LoadDotEnv(writeEnv(t, "DOTENV_A=1\n")); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_A"); got != "1" {
		t.Fatalf("DOTENV_A = %q", got)
	}
}

func TestLoadDotEnvSkipsCommentsAndBlanks(t *testing.T) {
	unset(t, "DOTENV_B")
	if err := LoadDotEnv(writeEnv(t, "# comment\n\n  \nDOTENV_B=2\n")); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_B"); got != "2" {
		t.Fatalf("DOTENV_B = %q", got)
	}
}

func TestLoadDotEnvStripsQuotes(t *testing.T) {
	unset(t, "DOTENV_C")
	if err := LoadDotEnv(writeEnv(t, `DOTENV_C="x y"`+"\n")); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_C"); got != "x y" {
		t.Fatalf("DOTENV_C = %q", got)
	}
}

func TestLoadDotEnvKeepsEqualsInValue(t *testing.T) {
	unset(t, "DOTENV_URL")
	if err := LoadDotEnv(writeEnv(t, "DOTENV_URL=a=b\n")); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_URL"); got != "a=b" {
		t.Fatalf("DOTENV_URL = %q", got)
	}
}

func TestLoadDotEnvExistingWins(t *testing.T) {
	t.Setenv("DOTENV_D", "env")
	if err := LoadDotEnv(writeEnv(t, "DOTENV_D=file\n")); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_D"); got != "env" {
		t.Fatalf("DOTENV_D = %q", got)
	}
}

func TestLoadDotEnvMalformedLine(t *testing.T) {
	err := LoadDotEnv(writeEnv(t, "A=1\nB=2\nNOEQUALS\n"))
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v, want mention of line 3", err)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}
