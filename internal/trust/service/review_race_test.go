package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
	"github.com/shopnexus/server/internal/trust/service"
)

// rivalFirst is a review store that runs rival once, right before the
// first insert or save, as another request that wrote in between.
type rivalFirst struct {
	repository.Reviews

	rival func(context.Context) error
	ran   atomic.Bool
}

func (s *rivalFirst) race(ctx context.Context) error {
	if s.ran.Swap(true) {
		return nil
	}
	return s.rival(ctx)
}

func (s *rivalFirst) Insert(ctx context.Context, r domain.Review) error {
	if err := s.race(ctx); err != nil {
		return err
	}
	return s.Reviews.Insert(ctx, r)
}

func (s *rivalFirst) Save(ctx context.Context, r *domain.Review, writeID uuid.UUID) error {
	if err := s.race(ctx); err != nil {
		return err
	}
	return s.Reviews.Save(ctx, r, writeID)
}

// racing is a second trust on e's database whose first write loses to
// rival, a write e.trust makes in between.
func (e env) racing(rival func(context.Context) error) *service.Trust {
	deps := e.deps
	deps.Reviews = &rivalFirst{Reviews: deps.Reviews, rival: rival}
	return service.New(deps)
}

// A write that loses to another on the same review gets try_again and
// writes nothing; reading again shows the other write, and the next try
// goes through. A twin of the same write is answered as it landed.
//
// UC-TRS-01 E9 · NFR-CONC-01.
func TestAReviewWriteThatLosesARace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	own := func(t *testing.T, caller, order uuid.UUID) service.OwnReview {
		t.Helper()
		m, err := e.trust.OrderReviews(ctx, caller, []uuid.UUID{order})
		if err != nil {
			t.Fatal(err)
		}
		return m[order].Own
	}
	put := func(caller, order uuid.UUID, stars int, text string) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := e.trust.PutReview(ctx, caller, order, stars, text, nil, nil)
			return err
		}
	}

	t.Run("first review", func(t *testing.T) {
		t.Parallel()
		seller, buyer := e.person(t), e.person(t)
		_, order := e.completed(t, buyer, seller)
		raced := e.racing(put(buyer, order, 3, "Tạm được"))
		if _, err := raced.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil); !errors.Is(err, apperr.ErrTryAgain) {
			t.Fatalf("lost: %v", err)
		}
		if r := own(t, buyer, order); r.Stars == nil || *r.Stars != 3 {
			t.Fatalf("read again: %+v", r)
		}
		if v, err := raced.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil); err != nil || v.Stars != 5 {
			t.Fatalf("next try: %+v %v", v, err)
		}
	})

	t.Run("twin", func(t *testing.T) {
		t.Parallel()
		seller, buyer := e.person(t), e.person(t)
		_, order := e.completed(t, buyer, seller)
		raced := e.racing(put(buyer, order, 5, "Đúng mô tả"))
		if v, err := raced.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil); err != nil || v.Stars != 5 {
			t.Fatalf("twin: %+v %v", v, err)
		}
	})

	t.Run("edit", func(t *testing.T) {
		t.Parallel()
		seller, buyer := e.person(t), e.person(t)
		_, order := e.completed(t, buyer, seller)
		if err := put(buyer, order, 5, "Đúng mô tả")(ctx); err != nil {
			t.Fatal(err)
		}
		raced := e.racing(put(buyer, order, 3, "Hơi trầy"))
		if _, err := raced.PutReview(ctx, buyer, order, 4, "Pin yếu", nil, nil); !errors.Is(err, apperr.ErrTryAgain) {
			t.Fatalf("lost: %v", err)
		}
		if r := own(t, buyer, order); r.Stars == nil || *r.Stars != 3 || *r.Text != "Hơi trầy" {
			t.Fatalf("read again: %+v", r)
		}
		if v, err := raced.PutReview(ctx, buyer, order, 4, "Pin yếu", nil, nil); err != nil || v.Stars != 4 {
			t.Fatalf("next try: %+v %v", v, err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		seller, buyer := e.person(t), e.person(t)
		_, order := e.completed(t, buyer, seller)
		if err := put(buyer, order, 5, "Đúng mô tả")(ctx); err != nil {
			t.Fatal(err)
		}
		raced := e.racing(put(buyer, order, 3, "Hơi trầy"))
		if err := raced.DeleteReview(ctx, buyer, order); !errors.Is(err, apperr.ErrTryAgain) {
			t.Fatalf("lost: %v", err)
		}
		if r := own(t, buyer, order); r.Stars == nil || *r.Stars != 3 {
			t.Fatalf("read again: %+v", r)
		}
		if err := raced.DeleteReview(ctx, buyer, order); err != nil {
			t.Fatalf("next try: %v", err)
		}
		if r := own(t, buyer, order); r.State == "active" {
			t.Fatalf("after the delete: %+v", r)
		}
	})

	t.Run("reply", func(t *testing.T) {
		t.Parallel()
		seller, buyer := e.person(t), e.person(t)
		_, order := e.completed(t, buyer, seller)
		if err := put(seller, order, 4, "Nhận đúng hẹn")(ctx); err != nil {
			t.Fatal(err)
		}
		first, err := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		raced := e.racing(func(ctx context.Context) error {
			_, rerr := e.trust.ReplyReview(ctx, seller, first.ID, "Cảm ơn bạn")
			return rerr
		})
		if _, err = raced.ReplyReview(ctx, seller, first.ID, "Cảm ơn"); !errors.Is(err, apperr.ErrTryAgain) {
			t.Fatalf("lost: %v", err)
		}
		if r := own(t, buyer, order); r.Reply == nil || r.Reply.Text != "Cảm ơn bạn" {
			t.Fatalf("read again: %+v", r)
		}
		if _, err = raced.ReplyReview(ctx, seller, first.ID, "Cảm ơn"); !errors.Is(err, domain.ErrReplied) {
			t.Fatalf("next try: %v", err)
		}
	})
}
