package slice

func Diff[T comparable](a, b []T) (added, removed []T) {
	aMap := make(map[T]struct{}, len(a))

	for _, item := range a {
		aMap[item] = struct{}{}
	}

	// Track items that are in b but not in a
	for _, item := range b {
		if _, exists := aMap[item]; !exists {
			added = append(added, item)
		} else {
			// Remove from aMap to avoid unnecessary iteration later
			delete(aMap, item)
		}
	}

	// Remaining items in aMap are the removed ones
	for item := range aMap {
		removed = append(removed, item)
	}

	return added, removed
}

func Map[T, U any](slice []T, transform func(T) U) []U {
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = transform(v)
	}
	return result
}

func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := make([]T, 0)
	for _, v := range slice {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}

func FilterMap[T any, U any](slice []T, transform func(T) (U, bool)) []U {
	result := make([]U, 0)
	for _, v := range slice {
		if mapped, ok := transform(v); ok {
			result = append(result, mapped)
		}
	}
	return result
}

func NewMap[T any, G comparable](items []T, keyFunc func(T) G) map[G]*T {
	m := make(map[G]*T)
	for _, item := range items {
		k := keyFunc(item)
		m[k] = &item
	}
	return m
}

type SliceMapID[T any, G comparable] struct {
	Map map[G]*T
	IDs []G
}

func NewSliceMapID[T any, G comparable](items []T, keyFunc func(T) G) *SliceMapID[T, G] {
	m := make(map[G]*T)
	ids := make([]G, 0, len(items))
	for _, item := range items {
		k := keyFunc(item)
		m[k] = &item
		ids = append(ids, k)
	}
	return &SliceMapID[T, G]{
		Map: m,
		IDs: ids,
	}
}

func MapToSlice[T any, G comparable](m map[G]T) []T {
	result := make([]T, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

func NonNil[T any](slice []T) []T {
	if len(slice) == 0 {
		return make([]T, 0)
	}
	return slice
}
