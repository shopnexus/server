// Package geoip names the country of an IP address from a database in
// MaxMind's format, e.g. ip-location-db's geo-whois-asn-country.
package geoip

import (
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

// DB answers country lookups. A nil DB knows no country.
type DB struct {
	r *maxminddb.Reader
}

// Open reads the database at path; an empty path gives a nil DB.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // A nil DB is how no database looks.
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &DB{r: r}, nil
}

// Country is the ISO 3166 code of ip's country, or "" when unknown.
func (db *DB) Country(ip netip.Addr) string {
	if db == nil || !ip.IsValid() {
		return ""
	}
	// ip-location-db writes country_code; MaxMind's own databases nest it.
	var rec struct {
		Code    string `maxminddb:"country_code"`
		Country struct {
			ISO string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := db.r.Lookup(ip.Unmap()).Decode(&rec); err != nil {
		return ""
	}
	if rec.Code != "" {
		return rec.Code
	}
	return rec.Country.ISO
}

// Close releases the database.
func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	return db.r.Close() //nolint:wrapcheck // Close has nothing to add.
}
