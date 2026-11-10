package service

import (
	"context"
	"fmt"
	"uuid"
)

// DeleteAccount deletes a deleted account's reads. A read still running
// finds its row gone at its next save and consumes its video then.
func (a *Assist) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if err := a.d.Reads.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete reads: %w", err)
	}
	return nil
}
