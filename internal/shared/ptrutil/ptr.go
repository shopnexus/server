package ptrutil

// ToPtr returns a pointer to val if valid is true, or nil otherwise.
func PtrIf[T any](val T, valid bool) *T {
	if !valid {
		return nil
	}
	return &val
}
