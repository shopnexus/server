package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

func TestRecordAndRecords(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if r, err := at(s, now).Record(context.Background(), id); err != nil || r.ID != id || r.DisplayName != "Linh" {
		t.Fatalf("record: %+v %v", r, err)
	}
	if rs, err := at(s, now).Records(context.Background(), page.Request{Cursor: uuid.UUID{}, Limit: 5}); err != nil || len(rs) != 1 || rs[0].ID != id {
		t.Fatalf("records: %+v %v", rs, err)
	}
}
