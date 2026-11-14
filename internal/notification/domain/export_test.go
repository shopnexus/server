package domain

// Described reports whether t has an entry in types.
func Described(t Type) bool {
	_, ok := types[t]
	return ok
}
