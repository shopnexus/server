package sharedmodel

type OptionMethod string

type OptionConfig struct {
	ID          string       // e.g. "ghn-standard", "ghtk-express", "vnpay-qr", "cod"
	Provider    string       // "ghn", "ghtk", "vnpost", "vnpay", "momo"
	Method      OptionMethod // "standard", "express" "qr", "bank", "atm", "cod"
	Name        string       // e.g. "Giao hàng nhanh - Standard", "Giao hàng tiết kiệm - Express", "VNPay - QR", "COD"
	Description string
}
