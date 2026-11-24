// Package repository stores module feedback's rows and their flat groups.
package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
)

// Feedback is one feedback as stored. RootID is its group's root, its own
// ID for a root; Others counts the rest of the group.
type Feedback struct {
	ID             uuid.UUID
	AccountID      uuid.UUID
	Mood           int
	Kind           domain.Kind
	Text           string
	Photos         []domain.Photo
	Device         *domain.Device
	Screen         string
	Contact        bool
	Source         domain.Source
	PageURL        string
	Status         domain.Status
	StatusNote     string
	ShippedVersion string
	StatusAt       time.Time
	RootID         uuid.UUID
	Others         int
	Reply          string
	RepliedAt      *time.Time
	Hearted        bool
	CreatedAt      time.Time
}

// Group is a group's root with its figures: its size, how many gave each
// mood (Moods[0] is mood 1), how many came from the app and the web, and
// how many were sent on each of the last seven days, oldest first.
type Group struct {
	Root  Feedback
	Size  int
	Moods [5]int
	App   int
	Web   int
	Days  [7]int
}

// Summary is what was sent in a period: per day, oldest first, the mood's
// mean (0 for a day with none) and the count; the mood's mean over the
// period and over the one before it (0 for none); the counts by source and
// kind; the feedback still sent; the groups shipped in it and the feedback
// in them; and the screens sent from most, with their counts.
type Summary struct {
	DayMood       []float64
	DaySent       []int
	Mood          float64
	MoodBefore    float64
	Sent          int
	App           int
	Web           int
	Bugs          int
	Ideas         int
	Praise        int
	Waiting       int
	ShippedGroups int
	ShippedPeople int
	Screens       []Screen
}

// Screen is an app screen or a website route and how much was sent from it.
type Screen struct {
	Screen string
	Count  int
}

// Store stores feedback. The writes on groups (SetStatus, Join, Forget)
// take one lock for all groups, so two of them never interleave; staff
// make few.
type Store interface {
	// Insert stores f, sent, unless the account sent domain.DailyLimit
	// feedback after since (domain.ErrTooMany) or is deleted
	// (accountdomain.ErrSessionRevoked). A key the account used already
	// returns that feedback and true instead.
	Insert(ctx context.Context, f Feedback, key uuid.UUID, since time.Time) (Feedback, bool, error)
	// ByKey reads the feedback account sent under key; domain.ErrNotFound
	// for none.
	ByKey(ctx context.Context, account, key uuid.UUID) (Feedback, error)
	// SentSince counts the feedback account sent after since, without the
	// lock: the check before Insert, which counts again under it.
	SentSince(ctx context.Context, account uuid.UUID, since time.Time) (int, error)
	// Get reads a feedback; domain.ErrNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (Feedback, error)
	// OfAccount reads account's newest feedback, at most limit.
	OfAccount(ctx context.Context, account uuid.UUID, limit int) ([]Feedback, error)
	// Queue reads feedback of status and kind, any when nil, newest first,
	// below p's cursor, at most p.Fetch().
	Queue(ctx context.Context, status *domain.Status, kind *domain.Kind, p page.Request) ([]Feedback, error)
	// Group reads the feedback of root's group, the root included, newest
	// first, at most limit.
	Group(ctx context.Context, root uuid.UUID, limit int) ([]Feedback, error)
	// Groups reads the roots of kind, any when nil, largest group first,
	// at most limit, each with its group's figures up to now.
	Groups(ctx context.Context, kind *domain.Kind, limit int, now time.Time) ([]Group, error)
	// Summary counts the feedback sent in [since, now).
	Summary(ctx context.Context, since, now time.Time) (Summary, error)
	// SetStatus sets c on id's whole group, status_at moving where the
	// status changed, with a feedback.moved event for each row that moved
	// to a status that tells, and the audit row. It returns id's row.
	SetStatus(ctx context.Context, id uuid.UUID, c domain.StatusChange, actor audit.Actor, at time.Time) (Feedback, error)
	// Reply sets ShopNexus's reply, replacing an earlier one, with the audit
	// row; the first reply also writes feedback.replied.
	Reply(ctx context.Context, id uuid.UUID, text string, actor audit.Actor, at time.Time) (Feedback, error)
	// Heart sets whether staff hearted id, with the audit row when it
	// changed.
	Heart(ctx context.Context, id uuid.UUID, hearted bool, actor audit.Actor, at time.Time) (Feedback, error)
	// Join moves id, and the feedback joined to it when it is a root, into
	// other's group, where they take the group's status, note and version,
	// as SetStatus does. Already one group changes nothing.
	Join(ctx context.Context, id, other uuid.UUID, actor audit.Actor, at time.Time) (Feedback, error)
	// Forget deletes account's feedback. A group whose root it deletes gets
	// its oldest remaining feedback as root.
	Forget(ctx context.Context, account uuid.UUID) error
}
