package account

import (
	"time"
	"uuid"
)

// AccountView is the Account schema in api/schemas/account.yaml.
type AccountView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

func viewOf(a Account, now time.Time) AccountView {
	return AccountView{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
}
