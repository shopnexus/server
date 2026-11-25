package domain

import (
	"strings"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// conditionLabel is the field a Chợ Tốt listing states its condition in.
const conditionLabel = "tình trạng"

// adLabels are Chợ Tốt's names of fields that a category of ShopNexus
// names otherwise, lowercase.
var adLabels = map[string]string{
	"màu sắc":          "Màu",
	"hãng xe":          "Hãng",
	"bộ vi xử lý":      "CPU",
	"card màn hình":    "Card đồ họa",
	"kích cỡ màn hình": "Kích thước",
}

// AdCondition reads a Chợ Tốt listing's condition from its fields; false
// when it states none ShopNexus can tell.
func AdCondition(fields []catalogdomain.Spec) (catalogdomain.Condition, bool) {
	for _, f := range fields {
		if strings.ToLower(strings.TrimSpace(f.Label)) != conditionLabel {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(f.Value))
		switch {
		case strings.Contains(v, "như mới") || strings.Contains(v, "99%"):
			return catalogdomain.ConditionLikeNew, true
		case strings.HasPrefix(v, "mới"):
			return catalogdomain.ConditionNew, true
		case strings.Contains(v, "đã sử dụng") || strings.Contains(v, "cũ"):
			return catalogdomain.ConditionUsed, true
		}
	}
	return "", false
}

// AdSpecs are a Chợ Tốt listing's fields as specs under ShopNexus's names,
// the condition left out, for catalog to key.
func AdSpecs(fields []catalogdomain.Spec) []catalogdomain.Spec {
	out := make([]catalogdomain.Spec, 0, len(fields))
	for _, f := range fields {
		label := strings.ToLower(strings.TrimSpace(f.Label))
		if label == conditionLabel {
			continue
		}
		name := strings.TrimSpace(f.Label)
		if n, ok := adLabels[label]; ok {
			name = n
		}
		out = append(out, catalogdomain.Spec{Label: name, Value: strings.TrimSpace(f.Value)})
	}
	return out
}
