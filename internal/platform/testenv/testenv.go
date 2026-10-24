// Package testenv reads the test stack's settings from .env.test at the
// repository root. A variable already set in the environment wins, so a CI
// job can point the tests elsewhere.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/shopnexus/server2/internal/platform/config"
)

var load = sync.OnceValue(func() error {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", ".env.test")
	if err := config.LoadDotEnv(path); err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
})

// Get returns the variable key, or an error naming it when neither the
// environment nor .env.test sets it.
func Get(key string) (string, error) {
	if err := load(); err != nil {
		return "", err
	}
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("%s is not set; .env.test sets it", key)
	}
	return v, nil
}
