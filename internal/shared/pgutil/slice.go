package pgutil

import (
	"github.com/guregu/null/v6"
)

// SliceToPgArray converts a slice of values to a pgtype array
func SliceToPgArray[T any, P any](slice []T, converter func(T) P) []P {
	//! We dont use this: result := make([]P, len(slice))
	// This will make the sqlc.slice recognize the array not null
	var result []P
	for i := range slice {
		result = append(result, converter(slice[i]))
	}
	return result
}

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
