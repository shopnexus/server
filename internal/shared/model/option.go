package sharedmodel

import (
	"encoding/json"

	"github.com/google/uuid"
)

// OptionConfig represents the configuration for a payment option, which is a specific way to pay within a payment provider.
//
// Note: not in common model because if usage in provider interface.
type OptionConfig struct {
	ID          string          `json:"id"`                 // e.g. "ghtk-express", "vnpay-qr", "sepay-bank-transfer"
	Provider    string          `json:"provider"`           // "ghtk", "vnpay", "sepay", "card"
	Name        string          `json:"name"`               // e.g. "VNPay - QR", "SePay - Bank Transfer"
	Description string          `json:"description"`        // e.g. "VNPay QR payment method"
	Priority    int32           `json:"priority,omitempty"` // display order
	Config      json.RawMessage `json:"config,omitempty"`   // provider-specific config
	LogoRsID    uuid.NullUUID   `json:"logo_rs_id"`         // logo resource ID
}
