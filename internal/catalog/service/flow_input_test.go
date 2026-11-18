package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/catalog/service"
)

// The flows' inputs keep the wire key "draft": during a rolling deploy an old
// pod sends it to the new code and the new code's sends reach old pods, so a
// rename would post an empty listing.
func TestFlowInputsKeepTheDraftKey(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{"post": service.PostInput{}, "edit": service.EditInput{}} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"draft":`) {
			t.Errorf("%s input: %s", name, b)
		}
	}
}
