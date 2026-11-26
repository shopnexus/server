// Package service holds module feedback's use cases: a person sends
// feedback and reads what became of it; staff set a group's status, reply,
// heart and join feedback into groups. Every one is a direct use case.
package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
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
	//
	// UC-MOD-11 · NFR-IDEM-01
	Send(ctx context.Context, account, key uuid.UUID, in domain.Draft) (FeedbackView, bool, error)
	// Mine lists account's newest feedback, at most MineLimit.
	//
	// UC-MOD-11
	Mine(ctx context.Context, account uuid.UUID) ([]FeedbackView, error)
	// Facts reads what notification tells of a feedback; false for none.
	Facts(ctx context.Context, id uuid.UUID) (Facts, bool, error)
	// AccountDeleted deletes a deleted account's feedback.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
	// Queue lists feedback of status and kind, any when nil, newest first,
	// for staff.
	//
	// UC-MOD-11
	Queue(ctx context.Context, status *domain.Status, kind *domain.Kind, p page.Request) (page.Page[ConsoleFeedback], error)
	// Detail reads id with the rest of its group, newest first, at most
	// GroupLimit.
	//
	// UC-MOD-11
	Detail(ctx context.Context, id uuid.UUID) (ConsoleFeedbackDetail, error)
	// Groups lists the groups of kind, any when nil, largest first, at most
	// GroupsLimit.
	//
	// UC-MOD-11
	Groups(ctx context.Context, kind *domain.Kind) ([]GroupView, error)
	// Summary counts the feedback of the last SummaryDays days.
	Summary(ctx context.Context) (SummaryView, error)
	// SetStatus sets a status, note and version on id's whole group.
	//
	// UC-MOD-11 · UC-SHR-02
	SetStatus(ctx context.Context, id uuid.UUID, in StatusInput, by audit.Actor) (ConsoleFeedback, error)
	// Reply sets ShopNexus's one reply to id; a second call replaces it.
	//
	// UC-MOD-11 · UC-SHR-02
	Reply(ctx context.Context, id uuid.UUID, text string, by audit.Actor) (ConsoleFeedback, error)
	// Heart sets whether staff hearted id.
	//
	// UC-MOD-11 · UC-SHR-02
	Heart(ctx context.Context, id uuid.UUID, hearted bool, by audit.Actor) (ConsoleFeedback, error)
	// SameAs joins id to other's group.
	//
	// UC-MOD-11 · UC-SHR-02
	SameAs(ctx context.Context, id, other uuid.UUID, by audit.Actor) (ConsoleFeedback, error)
}

const (
	// MineLimit is how much feedback Mine lists.
	MineLimit = 100
	// GroupLimit is how much of a group Detail reads.
	GroupLimit = 50
	// GroupsLimit is how many groups Groups lists.
	GroupsLimit = 100
	// SummaryDays is the period Summary counts.
	SummaryDays = 30
)

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

// PersonView is the FeedbackPerson schema: who sent a feedback.
type PersonView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	// Contact is the phone number, or the email address without one.
	Contact string               `json:"contact,omitempty"`
	Status  accountdomain.Status `json:"status"`
}

// ConsoleFeedback is the ConsoleFeedback schema.
type ConsoleFeedback struct {
	Feedback  FeedbackView   `json:"feedback"`
	AccountID uuid.UUID      `json:"account_id"`
	Person    PersonView     `json:"person"`
	Device    *domain.Device `json:"device,omitempty"`
	Screen    string         `json:"screen,omitempty"`
	Contact   bool           `json:"contact"`
	Source    domain.Source  `json:"source"`
	PageURL   string         `json:"page_url,omitempty"`
	RootID    uuid.UUID      `json:"root_id"`
}

// ConsoleFeedbackDetail is the ConsoleFeedbackDetail schema.
type ConsoleFeedbackDetail struct {
	Item  ConsoleFeedback   `json:"item"`
	Group []ConsoleFeedback `json:"group"`
}

// GroupView is the FeedbackGroup schema.
type GroupView struct {
	Root  ConsoleFeedback `json:"root"`
	Size  int             `json:"size"`
	Moods [5]int          `json:"moods"`
	App   int             `json:"app"`
	Web   int             `json:"web"`
	Days  [7]int          `json:"days"`
}

// SummaryView is the FeedbackSummary schema; a mood of 0 is none.
type SummaryView = repository.Summary

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
	Store    repository.Store
	Media    mediasvc.Service
	Accounts accountsvc.Service
	Now      func() time.Time
}

// Feedback implements Service in this process.
type Feedback struct {
	d Deps
}

var _ Service = (*Feedback)(nil)

func New(d Deps) *Feedback { return &Feedback{d: d} }

// view shows f with its photos' public addresses.
func (s *Feedback) view(f repository.Feedback) FeedbackView {
	v := FeedbackView{
		ID: f.ID, Mood: f.Mood, Kind: f.Kind, Text: f.Text, Photos: make([]PhotoView, 0, len(f.Photos)), Status: f.Status,
		StatusNote: f.StatusNote, ShippedVersion: f.ShippedVersion, Others: f.Others, Hearted: f.Hearted,
		CreatedAt: f.CreatedAt, StatusAt: f.StatusAt,
	}
	for _, p := range f.Photos {
		v.Photos = append(v.Photos, PhotoView{UploadID: p.UploadID, URL: s.d.Media.URL(p.PhotoKey), ThumbURL: s.d.Media.URL(p.ThumbKey)})
	}
	if f.RepliedAt != nil {
		v.Reply = &ReplyView{Text: f.Reply, At: *f.RepliedAt}
	}
	return v
}

// consoles shows fs for staff, with their persons read in one query each.
func (s *Feedback) consoles(ctx context.Context, fs []repository.Feedback) ([]ConsoleFeedback, error) {
	ids := make([]uuid.UUID, 0, len(fs))
	for i := range fs {
		ids = append(ids, fs[i].AccountID)
	}
	accounts, err := s.d.Accounts.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the persons: %w", err)
	}
	contacts, err := s.d.Accounts.Contacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the persons' contacts: %w", err)
	}
	out := make([]ConsoleFeedback, 0, len(fs))
	for i := range fs {
		f, a := fs[i], accounts[fs[i].AccountID]
		out = append(out, ConsoleFeedback{
			Feedback: s.view(f), AccountID: f.AccountID, Device: f.Device, Screen: f.Screen, Contact: f.Contact,
			Source: f.Source, PageURL: f.PageURL, RootID: f.RootID,
			Person: PersonView{ID: f.AccountID, DisplayName: a.DisplayName, Contact: contacts[f.AccountID], Status: a.Status},
		})
	}
	return out, nil
}

// console shows f for staff.
func (s *Feedback) console(ctx context.Context, f repository.Feedback) (ConsoleFeedback, error) {
	out, err := s.consoles(ctx, []repository.Feedback{f})
	if err != nil {
		return ConsoleFeedback{}, err
	}
	return out[0], nil
}
