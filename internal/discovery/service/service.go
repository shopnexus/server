// Package service holds module discovery's use cases: a person saves
// listings and reads them back, and searches.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Service is module discovery's contract.
type Service interface {
	// Save keeps someone else's listing on sale or being shipped, with its
	// price; saving it again changes nothing.
	Save(ctx context.Context, account, listing uuid.UUID) error
	Unsave(ctx context.Context, account, listing uuid.UUID) error
	// IsSaved reports whether account saved listing.
	IsSaved(ctx context.Context, account, listing uuid.UUID) (bool, error)
	// Saved lists the account's saves, newest first.
	Saved(ctx context.Context, account uuid.UUID, p page.Request) (page.Page[SavedView], error)
	// Search finds active listings; viewer, nil when signed out, sees which
	// they saved.
	Search(ctx context.Context, viewer *uuid.UUID, in SearchInput) (SearchView, error)
	// History is the account's last domain.MaxHistory text searches,
	// newest first.
	History(ctx context.Context, account uuid.UUID) (HistoryView, error)
	// ForgetSearch deletes one entry of the account's history; an unknown
	// one is fine.
	ForgetSearch(ctx context.Context, account, id uuid.UUID) error
	// ClearHistory empties the account's history.
	ClearHistory(ctx context.Context, account uuid.UUID) error
	// Hide leaves someone else's listing out of account's searches;
	// hiding it again makes it the newest hide.
	Hide(ctx context.Context, account, listing uuid.UUID) error
	// Unhide shows it again; a listing not hidden is fine.
	Unhide(ctx context.Context, account, listing uuid.UUID) error
	// AccountDeleted deletes what discovery keeps of a deleted account: its
	// saves, history, hides, watches and settings. The worker calls it on
	// account.deleted.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
}

// Deps is what New builds Discovery from.
type Deps struct {
	Saves          repository.Saves
	Understandings repository.Understandings
	History        repository.History
	Hidden         repository.Hidden
	Catalog        catalogsvc.Service
	// Accounts gives the blocks a search leaves out.
	Accounts accountsvc.Service
	LLM            llm.Client
	// Media reads a search photo; Text and Image embed with the search key,
	// under TextVersion and ImageVersion. TextMin and PhotoMin are the
	// floors of the vector lists.
	Media                     mediasvc.Service
	QueryVectors              repository.QueryVectors
	PhotoVectors              repository.PhotoVectors
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	TextMin, PhotoMin         float64
	// ModelTimeout is domain.ModelTimeout in production; tests shorten it.
	ModelTimeout time.Duration
	Log          *slog.Logger
	Now          func() time.Time
}

// Discovery implements Service in this process.
type Discovery struct {
	saves                     repository.Saves
	understandings            repository.Understandings
	history                   repository.History
	hidden                    repository.Hidden
	catalog                   catalogsvc.Service
	accounts                  accountsvc.Service
	llm                       llm.Client
	media                     mediasvc.Service
	queryVectors              repository.QueryVectors
	photoVectors              repository.PhotoVectors
	text, image               embed.Model
	textVersion, imageVersion string
	textMin, photoMin         float64
	modelTimeout              time.Duration
	log                       *slog.Logger
	now                       func() time.Time
}

var _ Service = (*Discovery)(nil)

func New(d Deps) *Discovery {
	return &Discovery{
		saves: d.Saves, understandings: d.Understandings, history: d.History, hidden: d.Hidden, catalog: d.Catalog,
		accounts: d.Accounts, llm: d.LLM,
		media: d.Media, queryVectors: d.QueryVectors, photoVectors: d.PhotoVectors, text: d.Text, image: d.Image,
		textVersion: d.TextVersion, imageVersion: d.ImageVersion, textMin: d.TextMin, photoMin: d.PhotoMin,
		modelTimeout: d.ModelTimeout, log: d.Log, now: d.Now,
	}
}
