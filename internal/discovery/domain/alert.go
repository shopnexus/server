package domain

// AlertLevel is the price below which a save tells: its own level, else the
// price it was saved at.
func AlertLevel(below *int64, saved int64) int64 {
	if below != nil {
		return *below
	}
	return saved
}

// PriceStep is what a save's alert does at a new price. It tells at each
// new low below the level: below it and below the last price it told. A
// price at or above the level resets it, so the next drop tells again.
// Saves.PriceMoved runs the same rule in SQL.
func PriceStep(level int64, alerted *int64, price int64) (tell, reset bool) {
	if price >= level {
		return false, alerted != nil
	}
	return alerted == nil || price < *alerted, false
}
