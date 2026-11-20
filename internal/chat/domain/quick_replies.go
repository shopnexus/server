package domain

import "strings"

// DefaultQuickReplies are the quick replies of an account that never saved
// any: answers a seller often gives.
func DefaultQuickReplies() []string {
	return []string{
		"Còn bạn ơi.",
		"Bạn xem hàng giờ nào tiện? Mình rảnh buổi tối.",
		"Giá đã tốt rồi, mình bớt thêm không nhiều được bạn nhé.",
	}
}

// TrimQuickReplies returns each answer without the spaces around it, in the
// same order.
func TrimQuickReplies(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strings.TrimSpace(s)
	}
	return out
}
