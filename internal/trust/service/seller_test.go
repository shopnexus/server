package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/trust/service"
)

// The profile carries the account's public fields, the rating over every
// review, and the sold and on-sale counts: a reserved listing is on sale.
func TestSeller(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.shipped(t, buyer, seller)
	if _, err := e.trust.PutReview(ctx, buyer, order, 4, ""); err != nil {
		t.Fatal(err)
	}
	e.listing(t, seller)
	sold, err := e.listings.Get(ctx, e.listing(t, seller))
	if err != nil {
		t.Fatal(err)
	}
	if err = sold.MarkSold(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Save(ctx, &sold, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if _, err = e.accounts.Update(ctx, seller, accountsvc.UpdateInput{Bio: new("Đồ cũ còn tốt")}); err != nil {
		t.Fatal(err)
	}
	v, err := e.trust.Seller(ctx, seller)
	want := service.SellerView{
		ID: seller, DisplayName: "Linh", Bio: "Đồ cũ còn tốt", JoinedAt: v.JoinedAt,
		Rating: service.Rating{Average: 4, Count: 1}, Sold: 1, OnSale: 2,
	}
	if err != nil || v != want || v.JoinedAt.IsZero() {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.trust.Seller(ctx, uuid.NewV7()); !errors.Is(err, accountdomain.ErrAccountNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}
