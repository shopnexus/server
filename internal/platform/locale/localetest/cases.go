package localetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shopnexus/server/internal/platform/locale"
)

// Case is one case of locale's testdata/format-cases.json, a copy of the
// umbrella's docs/i18n/format-cases.json, which the app and the website
// test against too: an input and what each language writes of it.
type Case struct {
	Input json.RawMessage
	Want  map[locale.Tag]string
}

// FormatCases reads the cases of kind (money, money_short, decimal,
// day_month, ...) and fails t when there are none or a supported language
// lacks one.
func FormatCases(t *testing.T, kind string) []Case {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "testdata", "format-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []map[string]json.RawMessage `json:"cases"`
	}
	if err = json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	var out []Case
	for _, raw := range file.Cases {
		var k string
		if err = json.Unmarshal(raw["kind"], &k); err != nil || k != kind {
			continue
		}
		c := Case{Input: raw["input"], Want: map[locale.Tag]string{}}
		for _, tag := range locale.Supported {
			var s string
			if err = json.Unmarshal(raw[string(tag)], &s); err != nil {
				t.Fatalf("a %s case lacks %s: %v", kind, tag, err)
			}
			c.Want[tag] = s
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		t.Fatalf("no %s cases", kind)
	}
	return out
}
