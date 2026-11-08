package token

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewOpaque(t *testing.T) {
	t.Parallel()
	tok, hash := NewOpaque("rt_")
	again, _ := NewOpaque("rt_")
	if !strings.HasPrefix(tok, "rt_") || len(tok) != 3+43 || !bytes.Equal(hash, Hash(tok)) || again == tok {
		t.Fatalf("%q", tok)
	}
}
