package sepay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// signFields builds the SePay signature string from ordered fields and signs with HMAC-SHA256.
// Format: "field1=value1,field2=value2,..." → HMAC-SHA256 → base64.
func signFields(fields []keyValue, secret string) string {
	var parts []string
	for _, kv := range fields {
		if kv.value != "" {
			parts = append(parts, kv.key+"="+kv.value)
		}
	}
	data := strings.Join(parts, ",")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

type keyValue struct {
	key   string
	value string
}
