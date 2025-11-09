package slice

func EnsureSlice[T any](slice []T) []T {
	if len(slice) == 0 {
		return make([]T, 0)
	}
	return slice
}
