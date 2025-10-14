package pgutil

import "github.com/guregu/null/v6"

// SliceToPgArray converts a slice of values to a pgtype array
func SliceToPgArray[T any, P any](slice []T, converter func(T) P) []P {
	result := make([]P, len(slice))
	for i, item := range slice {
		result[i] = converter(item)
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
