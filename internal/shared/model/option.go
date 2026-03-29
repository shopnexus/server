package sharedmodel

type OptionMethod string

type OptionConfig struct {
	ID          string       `json:"id"`          // e.g. "ghn-standard", "ghtk-express", "vnpay-qr", "cod"
	Provider    string       `json:"provider"`    // "ghn", "ghtk", "vnpost", "vnpay", "momo"
	Method      OptionMethod `json:"method"`      // "standard", "express" "qr", "bank", "atm", "cod"
	Name        string       `json:"name"`        // e.g. "Giao hàng nhanh - Standard", "Giao hàng tiết kiệm - Express", "VNPay - QR", "COD"
	Description string       `json:"description"` // e.g. "Giao hàng nhanh standard service", "VNPay QR payment method"
}
