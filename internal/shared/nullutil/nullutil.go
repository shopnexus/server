package nullutil

import "github.com/guregu/null/v6"

// NullBoolToSlice converts a null.Bool to a slice of bool
func NullBoolToSlice(b null.Bool) []bool {
	if b.Valid {
		return []bool{b.Bool}
	}
	return nil
}

// NullInt64ToSlice converts a null.Int64 to a slice of int64
func NullInt64ToSlice(n null.Int64) []int64 {
	if n.Valid {
		return []int64{n.Int64}
	}
	return nil
}
