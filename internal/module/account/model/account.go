package accountmodel

import (
	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type Profile struct {
	ID          uuid.UUID `json:"id"`
	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`

	// Account base
	Type     accountdb.AccountType   `json:"type"`
	Status   accountdb.AccountStatus `json:"status"`
	Phone    null.String             `json:"phone"`
	Email    null.String             `json:"email"`
	Username null.String             `json:"username"`

	// Profile fields
	Gender           null.Value[accountdb.AccountGender] `json:"gender"`
	Name             null.String                         `json:"name"`
	DateOfBirth      null.Time                           `json:"date_of_birth"`
	EmailVerified    bool                                `json:"email_verified"`
	PhoneVerified    bool                                `json:"phone_verified"`
	DefaultContactID uuid.NullUUID                       `json:"default_contact_id"`
	AvatarURL        null.String                         `json:"avatar_url"`

	// Vendor fields
	Description null.String `json:"description"`
}
