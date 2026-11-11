package service

import (
	"context"
	"fmt"
	"uuid"
)

// DeleteAccount deletes a deleted account's reads. A read still running
// finds its row gone at its next save; a video read consumes its video
// then, and a link read's copies expire with their uploads.
func (a *Assist) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if err := a.d.VideoReads.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete video reads: %w", err)
	}
	if err := a.d.LinkReads.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete link reads: %w", err)
	}
	return nil
}
