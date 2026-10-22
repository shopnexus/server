package api_test

import (
	"testing"

	"github.com/shopnexus/server2/api"
)

func TestSpecValid(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Paths.Find("/healthz") == nil || doc.Paths.Find("/healthz").Get == nil {
		t.Fatal("/healthz GET missing after resolving $refs")
	}
}
