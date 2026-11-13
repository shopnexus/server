package carrier

// GHTKStatus maps GHTK's status_id; false for one that moves nothing:
// received, picking, delays, cancelled, and the shipper's notices.
func GHTKStatus(id int) (Status, bool) {
	switch id {
	case 3, 4:
		return StatusInTransit, true
	case 5, 6:
		return StatusDelivered, true
	case 9, 20, 21:
		return StatusFailed, true
	}
	return "", false
}

// GHNStatus maps GHN's order status; false for one that moves nothing.
func GHNStatus(s string) (Status, bool) {
	switch s {
	case "picked", "storing", "transporting", "sorting", "delivering", "money_collect_delivering":
		return StatusInTransit, true
	case "delivered":
		return StatusDelivered, true
	case "delivery_fail", "waiting_to_return", "return", "return_transporting", "return_sorting", "returning", "returned":
		return StatusFailed, true
	}
	return "", false
}
