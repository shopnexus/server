package restatex_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// TestRefusedJournalShape pins the JSON a step journals, so a replay of an
// older journal still decodes.
func TestRefusedJournalShape(t *testing.T) {
	t.Parallel()
	errGone := apperr.New(apperr.KindConflict, "gone", "the thing is gone").WithField("id", "gone")
	cases := []struct {
		in   restatex.Refused
		want string
	}{
		{restatex.Refused{}, `{}`},
		{
			restatex.Refused{Refusal: errGone},
			`{"refused":{"kind":4,"code":"gone","message":"the thing is gone","fields":{"id":"gone"}}}`,
		},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.in)
		if err != nil || string(got) != c.want {
			t.Fatalf("marshal: %s %v, want %s", got, err, c.want)
		}
		var back restatex.Refused
		if err = json.Unmarshal(got, &back); err != nil {
			t.Fatal(err)
		}
		if (back.Refusal == nil) != (c.in.Refusal == nil) ||
			(back.Refusal != nil && (!errors.Is(back.Refusal, errGone) || back.Refusal.Fields["id"] != "gone")) {
			t.Fatalf("round trip: %+v", back.Refusal)
		}
	}
}
