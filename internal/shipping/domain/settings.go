package domain

import (
	"uuid"

	"github.com/shopnexus/server/internal/platform/contact"
)

type Settings struct {
	Pickup *SavedPickup `json:"pickup"`
}

type SavedPickup struct {
	AddressID uuid.UUID `json:"address_id"`
	Phone     string    `json:"phone"`
}

func (s *Settings) Normalize() error {
	if s.Pickup == nil {
		return nil
	}
	phone, err := contact.ParsePhone(s.Pickup.Phone)
	if err != nil {
		return ErrInvalidSettings.WithField("phone", "Vietnamese mobile")
	}
	s.Pickup = &SavedPickup{AddressID: s.Pickup.AddressID, Phone: phone.Value()}
	return nil
}
