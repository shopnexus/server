// Package service holds module feedback's use cases: a person sends
// feedback and reads what became of it; staff set a group's status, reply,
// heart and join feedback into groups. Every one is a direct use case.
package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
)

// Service is module feedback's contract.
type Service interface {
	// Send stores account's feedback under key, or returns the feedback
	// the key made already, with existed true.
	Send(ctx context.Context, account, key uuid.UUID, in SendInput) (FeedbackView, bool, error)
	// Mine lists account's newest feedback, at most MineLimit.
	Mine(ctx context.Context, account uuid.UUID) ([]FeedbackView, error)
	// Facts reads what notification tells of a feedback; false for none.
	Facts(ctx context.Context, id uuid.UUID) (Facts, bool, error)
	// AccountDeleted deletes a deleted account's feedback.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
	// Queue lists feedback of status, any when nil, newest first, for staff.
	Queue(ctx context.Context, status *domain.Status, p page.Request) (page.Page[ConsoleFeedback], error)
	// SetStatus sets a status, note and version on id's whole group.
	SetStatus(ctx context.Context, id uuid.UUID, in StatusInput, by audit.Actor) (ConsoleFeedback, error)
	// Reply sets ShopNexus's one reply to id; a second call replaces it.
	Reply(ctx context.Context, id uuid.UUID, text string, by audit.Actor) (ConsoleFeedback, error)
	// Heart sets whether staff hearted id.
	Heart(ctx context.Context, id uuid.UUID, hearted bool, by audit.Actor) (ConsoleFeedback, error)
	// SameAs joins id to other's group.
	SameAs(ctx context.Context, id, other uuid.UUID, by audit.Actor) (ConsoleFeedback, error)
}

// MineLimit is how much feedback Mine lists.
const MineLimit = 100

// SendInput is the FeedbackDraft schema; Mood 0 is none.
type SendInput struct {
	Mood    int
	Kind    string
	Text    string
	Photos  []uuid.UUID
	Device  *domain.Device
	Screen  string
	Contact bool
}

// StatusInput is the ConsoleFeedbackStatus schema.
type StatusInput struct {
	Status  string
	Note    string
	Version string
}

// PhotoView is the FeedbackPhoto schema.
type PhotoView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url"`
	ThumbURL string    `json:"thumb_url"`
}

// ReplyView is the FeedbackReply schema.
type ReplyView struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// FeedbackView is the Feedback schema.
type FeedbackView struct {
	ID             uuid.UUID     `json:"id"`
	Mood           int           `json:"mood,omitempty"`
	Kind           domain.Kind   `json:"kind,omitempty"`
	Text           string        `json:"text,omitempty"`
	Photos         []PhotoView   `json:"photos"`
	Status         domain.Status `json:"status"`
	StatusNote     string        `json:"status_note,omitempty"`
	ShippedVersion string        `json:"shipped_version,omitempty"`
	Others         int           `json:"others"`
	Reply          *ReplyView    `json:"reply,omitempty"`
	Hearted        bool          `json:"hearted"`
	CreatedAt      time.Time     `json:"created_at"`
	StatusAt       time.Time     `json:"status_at"`
}

// ConsoleFeedback is the ConsoleFeedback schema.
type ConsoleFeedback struct {
	Feedback  FeedbackView   `json:"feedback"`
	AccountID uuid.UUID      `json:"account_id"`
	Device    *domain.Device `json:"device,omitempty"`
	Screen    string         `json:"screen,omitempty"`
	Contact   bool           `json:"contact"`
	RootID    uuid.UUID      `json:"root_id"`
}

// Facts are what a feedback notice is written from.
type Facts struct {
	ID      uuid.UUID     `json:"id"`
	Owner   uuid.UUID     `json:"owner"`
	Status  domain.Status `json:"status"`
	Version string        `json:"version,omitempty"`
	Reply   string        `json:"reply,omitempty"`
}

// Deps is what New builds Feedback from.
type Deps struct {
	Store repository.Store
	Media mediasvc.Service
	Now   func() time.Time
}

// Feedback implements Service in this process.
type Feedback struct {
	store repository.Store
	media mediasvc.Service
	now   func() time.Time
}

var _ Service = (*Feedback)(nil)

func New(d Deps) *Feedback { return &Feedback{store: d.Store, media: d.Media, now: d.Now} }

// view shows f with its photos' public addresses.
func (s *Feedback) view(f repository.Feedback) FeedbackView {
	v := FeedbackView{
		ID: f.ID, Mood: f.Mood, Kind: f.Kind, Text: f.Text, Photos: make([]PhotoView, 0, len(f.Photos)), Status: f.Status,
		StatusNote: f.StatusNote, ShippedVersion: f.ShippedVersion, Others: f.Others, Hearted: f.Hearted,
		CreatedAt: f.CreatedAt, StatusAt: f.StatusAt,
	}
	for _, p := range f.Photos {
		v.Photos = append(v.Photos, PhotoView{UploadID: p.UploadID, URL: s.media.URL(p.PhotoKey), ThumbURL: s.media.URL(p.ThumbKey)})
	}
	if f.RepliedAt != nil {
		v.Reply = &ReplyView{Text: f.Reply, At: *f.RepliedAt}
	}
	return v
}

// console shows f for staff.
func (s *Feedback) console(f repository.Feedback) ConsoleFeedback {
	return ConsoleFeedback{
		Feedback: s.view(f), AccountID: f.AccountID, Device: f.Device, Screen: f.Screen, Contact: f.Contact, RootID: f.RootID,
	}
}
