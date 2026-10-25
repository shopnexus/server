package domain

import (
	"testing"
	"time"
)

// With every argon2 slot taken, a hash waits until one frees.
//
//nolint:paralleltest // It holds every argon2 slot of the package.
func TestArgon2RunsAreBounded(t *testing.T) {
	for range cap(argonSlots) {
		argonSlots <- struct{}{}
	}
	done := make(chan struct{})
	go func() {
		Fast.Hash("p")
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("hashed while every slot was taken")
	case <-time.After(100 * time.Millisecond):
	}
	<-argonSlots
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not hash after a slot freed")
	}
	for range cap(argonSlots) - 1 {
		<-argonSlots
	}
}
