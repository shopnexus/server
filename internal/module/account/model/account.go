package accountmodel

import (
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type Profile struct {
	ID          uuid.UUID `json:"id"`
	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`

	// Account base
	Status   accountdb.AccountStatus `json:"status"`
	Phone    null.String             `json:"phone"`
	Email    null.String             `json:"email"`
	Username null.String             `json:"username"`

	// Profile fields
	Gender           *accountdb.AccountGender `json:"gender"`
	Name             null.String              `json:"name"`
	DateOfBirth      null.Time                `json:"date_of_birth"`
	EmailVerified    bool                     `json:"email_verified"`
	PhoneVerified    bool                     `json:"phone_verified"`
	Country          string                   `json:"country"`
	DefaultContactID uuid.NullUUID            `json:"default_contact_id"`
	AvatarURL        null.String              `json:"avatar_url"`

	// Description
	Description null.String `json:"description"`

	Settings ProfileSettings `json:"settings"`
}

// ProfileSettings is a typed view of account.profile.settings JSONB.
// Unknown fields in DB are preserved across updates via load-merge-write.
type ProfileSettings struct {
	PreferredCurrency string `json:"preferred_currency,omitempty"`
}
