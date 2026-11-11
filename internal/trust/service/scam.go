package service

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/domain"
)

// Classifier reads a redacted window of a conversation and picks the
// message codes of each side's own messages. The LLM is the first; a model
// trained on scam_checks can replace it with no other change.
type Classifier interface {
	// Name is stored in scam_checks.classifier, as "llm:scam-check".
	Name() string
	// PromptVersion is stored in scam_checks.prompt_version; 0 for a
	// classifier without a prompt.
	PromptVersion() int
	// Classify returns an error wrapping ErrRefused when the same window
	// would be refused again.
	Classify(ctx context.Context, w domain.Window) (domain.Verdict, error)
}

// FactSource tells which fact signals hold at now for the suspect of a
// window.
type FactSource interface {
	Signals(ctx context.Context, suspect uuid.UUID, w domain.Window, now time.Time) ([]domain.Signal, error)
}

// ErrRefused is wrapped by a classifier's error when the same window would
// be refused again; the check is skipped without a retry.
var ErrRefused = errors.New("trust: the classifier refused the window")
