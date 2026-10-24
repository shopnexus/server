package audit_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
)

type view struct {
	Status string     `json:"status"`
	Reason string     `json:"reason,omitempty"`
	Until  *time.Time `json:"until,omitempty"`
}

func TestDiffKeepsChangedFieldsOnly(t *testing.T) {
	t.Parallel()
	got := audit.Diff(view{Status: "active", Reason: "", Until: nil}, view{Status: "suspended", Reason: "spam", Until: nil})
	want := map[string]any{"status": []any{"active", "suspended"}, "reason": []any{nil, "spam"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiffOfACreation(t *testing.T) {
	t.Parallel()
	got := audit.Diff(nil, view{Status: "active", Reason: "", Until: nil})
	if !reflect.DeepEqual(got, map[string]any{"status": []any{nil, "active"}}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiffOfNoChange(t *testing.T) {
	t.Parallel()
	v := view{Status: "active", Reason: "", Until: nil}
	if got := audit.Diff(v, v); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
