package service

import (
	"context"
	"testing"
	"uuid"
)

// RefsFor adds the link cards to views as viewer sees them.
func RefsFor(t *testing.T, c *Chat, viewer uuid.UUID, views []MessageView) []MessageView {
	t.Helper()
	if err := c.refs(context.Background(), viewer, views); err != nil {
		t.Fatal(err)
	}
	return views
}
