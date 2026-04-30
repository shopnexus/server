package sharedmodel

import (
	"encoding/json"

	"github.com/google/uuid"
)

type OptionType string

const (
	OptionTypePayment     OptionType = "payment"
	OptionTypeTransport   OptionType = "transport"
	OptionTypeObjectStore OptionType = "object_store"
)

// Option represents the configuration for a payment option, which is a specific way to pay within a payment provider.
//
// Note: not in common model because we also use it in provider interface.
type Option struct {
	ID       string        `json:"id"`       // e.g. "ghtk-express", "vnpay-qr", "sepay-bank-transfer"
	OwnerID  uuid.NullUUID `json:"owner_id"` // if null, this option is provided by us; otherwise it's a user-provided option
	Type     OptionType    `json:"type"`     // e.g. "payment", "transport"
	Provider string        `json:"provider"` // "ghtk", "vnpay", "ghn", "stripe", ...

	IsEnabled   bool            `json:"is_enabled"`  // whether this option is currently enabled
	Name        string          `json:"name"`        // e.g. "VNPay - QR", "SePay - Bank Transfer"
	Description string          `json:"description"` // e.g. "VNPay QR payment method"
	Priority    int32           `json:"priority"`    // display order
	LogoRsID    uuid.NullUUID   `json:"logo_rs_id"`  // logo resource ID
	Data        json.RawMessage `json:"data"`        // provider-specific config
}
