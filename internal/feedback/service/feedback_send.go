package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/repository"
)

// Send checks the feedback, returns the key's feedback when there is one,
// refuses past the day's limit, processes the screenshots, then stores it.
// The key holds the photos in media, so a retry finds them its own. It is
// UC-MOD-11 steps 1 and 2.
func (s *Feedback) Send(ctx context.Context, account, key uuid.UUID, in domain.Draft) (FeedbackView, bool, error) {
	// E1
	d, err := domain.NewFeedback(in)
	if err != nil {
		return FeedbackView{}, false, err
	}
	// A1
	prior, err := s.d.Store.ByKey(ctx, account, key)
	if err == nil {
		return s.view(prior), true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return FeedbackView{}, false, fmt.Errorf("read the key's feedback: %w", err)
	}
	now := s.d.Now()
	since := now.Add(-domain.Window)
	n, err := s.d.Store.SentSince(ctx, account, since)
	if err != nil {
		return FeedbackView{}, false, fmt.Errorf("count feedback: %w", err)
	}
	// E2
	if n >= domain.DailyLimit {
		return FeedbackView{}, false, domain.ErrTooMany
	}
	photos := make([]domain.Photo, 0, len(d.Photos))
	if len(d.Photos) > 0 {
		processed, photoErr := s.d.Media.Photos(ctx, account, key, d.Photos)
		if photoErr != nil {
			return FeedbackView{}, false, fmt.Errorf("process the photos: %w", photoErr)
		}
		for _, id := range d.Photos {
			photos = append(photos, domain.Photo{UploadID: id, PhotoKey: processed[id].PhotoKey, ThumbKey: processed[id].ThumbKey})
		}
	}
	// 2. The feedback is stored as sent.
	f, existed, err := s.d.Store.Insert(ctx, repository.Feedback{
		ID: uuid.NewV7(), AccountID: account, Mood: d.Mood, Kind: d.Kind, Text: d.Text, Photos: photos, Device: d.Device,
		Screen: d.Screen, Contact: d.Contact, Source: d.Source, PageURL: d.PageURL, CreatedAt: now,
	}, key, since)
	if err != nil {
		return FeedbackView{}, false, fmt.Errorf("store the feedback: %w", err)
	}
	return s.view(f), existed, nil
}
