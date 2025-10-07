package accountmodel

import (
	"time"

	"shopnexus-remastered/internal/db"

	"github.com/guregu/null/v6"
)

type Profile struct {
	ID          int64     `json:"id"`
	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`

	// Account base
	Type     db.AccountType   `json:"type"`
	Status   db.AccountStatus `json:"status"`
	Phone    null.String      `json:"phone"`
	Email    null.String      `json:"email"`
	Username null.String      `json:"username"`

	// Profile fields
	Gender           null.Value[db.AccountGender] `json:"gender"`
	Name             null.String                  `json:"name"`
	DateOfBirth      time.Time                    `json:"date_of_birth"`
	AvatarRsID       null.Int64                   `json:"avatar_rs_id"`
	EmailVerified    bool                         `json:"email_verified"`
	PhoneVerified    bool                         `json:"phone_verified"`
	DefaultContactID null.Int64                   `json:"default_contact_id"`

	// Vendor fields
	Description null.String `json:"description"`
}
