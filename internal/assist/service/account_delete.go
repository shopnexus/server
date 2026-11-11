package service

import (
	"context"
	"fmt"
	"uuid"
)

// DeleteAccount deletes a deleted account's reads and waiting transcripts.
// A read still running finds its row gone at its next save; a video read
// consumes its video then, and copies and frames expire with their uploads.
func (a *Assist) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if err := a.d.Reads.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete reads: %w", err)
	}
	return nil
}
