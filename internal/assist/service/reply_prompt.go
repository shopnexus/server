package service

import "fmt"

// replyRules ends each read's instructions: the JSON reply. what names the
// source and notItem says when nothing in it is one item for sale.
func replyRules(what, notItem string) string {
	return fmt.Sprintf(`Reply with one JSON object and nothing else:
- "subject": a few words on what the %[1]s is of, written first, before you decide.
- "found": true when one item for sale is the %[1]s's subject, even when its brand, model or size cannot be told. Write
about that one item only. false only when %[2]s.
- "title" and "description": as above, in Vietnamese; both empty when found is false.
- "reason": empty when found is true. When found is false, one short sentence to the seller in Vietnamese with
diacritics: what the %[1]s shows instead and what to send, as in "Ảnh chỉ có một người, chưa thấy món đồ để bán. Chụp
riêng món đồ nhé."
Never ask the seller for more details and never refuse for want of them: write from what you have.
`, what, notItem)
}
