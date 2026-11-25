package domain

import "github.com/shopnexus/server/internal/platform/textkey"

// MaxHistory is how many searches a person's history keeps.
const MaxHistory = 10

// HistoryKey is a query as the history keys it: QueryKey, then without
// Vietnamese marks, đ as d, so "Xe Đạp" and "xe dap" are one entry.
func HistoryKey(q string) string { return textkey.Fold(q) }
