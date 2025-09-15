package accountmodel

import (
	"shopnexus-remastered/internal/db"
	"time"

	"github.com/guregu/null/v6"
)

type Profile struct {
	ID            int64                      `json:"id"`
	Type          db.AccountType             `json:"type"`
	Gender        null.Value[db.AccountType] `json:"gender"`
	Name          null.String                `json:"name"`
	DateOfBirth   time.Time                  `json:"date_of_birth"`
	AvatarRsID    null.Int64                 `json:"avatar_rs_id"`
	EmailVerified bool                       `json:"email_verified"`
	PhoneVerified bool                       `json:"phone_verified"`
	DateCreated   time.Time                  `json:"date_created"`
	DateUpdated   time.Time                  `json:"date_updated"`

	// Customer fields
	DefaultAddressID null.Int64 `json:"default_address_id,omitempty"`
	// Vendor fields
	Description null.String `json:"description,omitempty"`
}

//
//type PublicAccountProfile struct {
//	ID         int64             `json:"id"`
//	Gender     *db.AccountGender `json:"gender"`
//	Name       *string           `json:"name"`
//	AvatarRsID *int64            `json:"avatar_rs_id"`
//
//	DateCreated time.Time `json:"date_created"`
//	DateUpdated time.Time `json:"date_updated"`
//}
//
//type PrivateAccountProfile struct {
//	PublicAccountProfile
//	ID            int64     `json:"id"`
//	DateOfBirth   time.Time `json:"date_of_birth"`
//	EmailVerified bool      `json:"email_verified"`
//	PhoneVerified bool      `json:"phone_verified"`
//}
//
//type PublicCustomerProfile struct {
//	PublicAccountProfile
//}
//
//type PrivateCustomerProfile struct {
//	PrivateAccountProfile
//	DefaultAddressID *int64 `json:"default_address_id"`
//}
