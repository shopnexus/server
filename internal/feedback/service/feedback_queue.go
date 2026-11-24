package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

func (s *Feedback) Queue(ctx context.Context, status *domain.Status, kind *domain.Kind, p page.Request) (page.Page[ConsoleFeedback], error) {
	fs, err := s.store.Queue(ctx, status, kind, p)
	if err != nil {
		return page.Page[ConsoleFeedback]{}, fmt.Errorf("read feedback: %w", err)
	}
	out, err := s.consoles(ctx, fs)
	if err != nil {
		return page.Page[ConsoleFeedback]{}, err
	}
	return page.Of(out, p, func(c ConsoleFeedback) uuid.UUID { return c.Feedback.ID }), nil
}

func (s *Feedback) Detail(ctx context.Context, id uuid.UUID) (ConsoleFeedbackDetail, error) {
	f, err := s.store.Get(ctx, id)
	if err != nil {
		return ConsoleFeedbackDetail{}, fmt.Errorf("read the feedback: %w", err)
	}
	group, err := s.store.Group(ctx, f.RootID, GroupLimit+1)
	if err != nil {
		return ConsoleFeedbackDetail{}, fmt.Errorf("read the group: %w", err)
	}
	rows := []repository.Feedback{f}
	for i := range group {
		if group[i].ID != id && len(rows) <= GroupLimit {
			rows = append(rows, group[i])
		}
	}
	out, err := s.consoles(ctx, rows)
	if err != nil {
		return ConsoleFeedbackDetail{}, err
	}
	return ConsoleFeedbackDetail{Item: out[0], Group: out[1:]}, nil
}

func (s *Feedback) Groups(ctx context.Context, kind *domain.Kind) ([]GroupView, error) {
	gs, err := s.store.Groups(ctx, kind, GroupsLimit, s.now())
	if err != nil {
		return nil, fmt.Errorf("read groups: %w", err)
	}
	roots := make([]repository.Feedback, 0, len(gs))
	for i := range gs {
		roots = append(roots, gs[i].Root)
	}
	views, err := s.consoles(ctx, roots)
	if err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(gs))
	for i := range gs {
		g := gs[i]
		out = append(out, GroupView{Root: views[i], Size: g.Size, Moods: g.Moods, App: g.App, Web: g.Web, Days: g.Days})
	}
	return out, nil
}

func (s *Feedback) Summary(ctx context.Context) (SummaryView, error) {
	now := s.now()
	m, err := s.store.Summary(ctx, now.AddDate(0, 0, -SummaryDays), now)
	if err != nil {
		return SummaryView{}, fmt.Errorf("count feedback: %w", err)
	}
	screens := make([]ScreenCount, 0, len(m.Screens))
	for _, c := range m.Screens {
		screens = append(screens, ScreenCount{Screen: c.Screen, Count: c.Count})
	}
	return SummaryView{
		DayMood: m.DayMood, DaySent: m.DaySent, Mood: m.Mood, MoodBefore: m.MoodBefore, Sent: m.Sent, App: m.App, Web: m.Web,
		Bugs: m.Bugs, Ideas: m.Ideas, Praise: m.Praise, Waiting: m.Waiting, ShippedGroups: m.ShippedGroups,
		ShippedPeople: m.ShippedPeople, Screens: screens,
	}, nil
}
