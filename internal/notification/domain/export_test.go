package domain

import "github.com/shopnexus/server/internal/platform/locale"

// Described reports whether t has an entry in types.
func Described(t Type) bool {
	_, ok := types[t]
	return ok
}

// Words is notification's catalog.
var Words = words

// Money is writer.money in tag.
func Money(tag locale.Tag, n int64) string { return writerFor(tag).money(n) }
