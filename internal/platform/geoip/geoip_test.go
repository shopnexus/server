package geoip_test

import (
	"net/netip"
	"testing"

	"github.com/shopnexus/server/internal/platform/geoip"
)

func TestEmptyPathKnowsNoCountry(t *testing.T) {
	t.Parallel()
	db, err := geoip.Open("")
	if err != nil || db != nil {
		t.Fatalf("db %v, err %v", db, err)
	}
	if c := db.Country(netip.MustParseAddr("8.8.8.8")); c != "" {
		t.Fatalf("country %q", c)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFileFails(t *testing.T) {
	t.Parallel()
	if _, err := geoip.Open(t.TempDir() + "/none.mmdb"); err == nil {
		t.Fatal("no error")
	}
}
