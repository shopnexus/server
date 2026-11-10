package service_test

import (
	"context"
	"testing"
)

func TestTouch(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	svc := at(s, nil, now)
	id := seed(t, s)
	if err := svc.Touch(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Get(context.Background(), id)
	if err != nil || v.LastActiveAt == nil || !v.LastActiveAt.Equal(now) {
		t.Fatalf("%v %v", v.LastActiveAt, err)
	}
}
