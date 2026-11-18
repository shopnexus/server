package embed

import (
	"slices"
	"testing"
)

func TestPackAppendsEOSAndPads(t *testing.T) {
	t.Parallel()
	ids, n := pack([]int{10, 11, 12}, 1, 0)
	if n != 4 {
		t.Fatalf("tokens = %d, want 4", n)
	}
	if len(ids) != maxTextTokens {
		t.Fatalf("len = %d, want %d", len(ids), maxTextTokens)
	}
	if !slices.Equal(ids[:5], []int64{10, 11, 12, 1, 0}) || ids[maxTextTokens-1] != 0 {
		t.Errorf("ids = %v", ids[:6])
	}
}

func TestPackTruncatesKeepingEOSLast(t *testing.T) {
	t.Parallel()
	raw := make([]int, 100)
	for i := range raw {
		raw[i] = 100 + i
	}
	ids, n := pack(raw, 1, 0)
	if n != maxTextTokens {
		t.Fatalf("tokens = %d, want %d", n, maxTextTokens)
	}
	if ids[maxTextTokens-1] != 1 || ids[maxTextTokens-2] != int64(100+maxTextTokens-2) {
		t.Errorf("tail = %v", ids[maxTextTokens-3:])
	}
}

func TestPackEmptyTextIsEOSAlone(t *testing.T) {
	t.Parallel()
	ids, n := pack(nil, 1, 0)
	if n != 1 || ids[0] != 1 || ids[1] != 0 {
		t.Errorf("ids = %v, tokens = %d", ids[:3], n)
	}
}
