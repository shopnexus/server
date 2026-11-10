package domain

// AlertLevel is the price below which a save tells: its own level, else the
// saved price while the account's "Báo khi tin đã lưu giảm giá" is on. ok
// is false when the save has none.
func AlertLevel(below *int64, saved int64, switchOn bool) (level int64, ok bool) {
	switch {
	case below != nil:
		return *below, true
	case switchOn:
		return saved, true
	default:
		return 0, false
	}
}

// PriceStep is what a save's alert does at a new price. It tells at each
// new low below the level: below it and below the last price it told. A
// price at or above the level, or no level, resets it, so the next drop
// tells again. Saves.PriceMoved runs the same rule in SQL.
func PriceStep(level int64, ok bool, alerted *int64, price int64) (tell, reset bool) {
	if !ok || price >= level {
		return false, alerted != nil
	}
	return alerted == nil || price < *alerted, false
}
