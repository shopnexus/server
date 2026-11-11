package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
)

// ScamCheckName is the ScamCheck object's name without the prefix; trust's
// worker sends to it.
const ScamCheckName = "ScamCheck"

const (
	// classifyRetry and classifyAttempts bound the classify step; past
	// them the check is skipped.
	classifyRetry    = 10 * time.Minute
	classifyAttempts = 3
)

// ScamCheck reads a conversation's newest messages for scams. It is a
// Virtual Object keyed by the conversation ID: Restate runs one Run per
// conversation at a time, so a burst of messages queues and a later run
// finds its window checked already.
type ScamCheck struct {
	Prefix     string
	Chats      chatsvc.Service
	Scams      repository.ScamWarnings
	Classifier Classifier
	Facts      []FactSource
	Log        *slog.Logger
	Now        func() time.Time
}

func (s ScamCheck) ServiceName() string { return s.Prefix + ScamCheckName }

// checkPlan is what the plan step found: whether to run, the members, and
// the window's first and last message.
type checkPlan struct {
	Run      bool      `json:"run"`
	AccountA uuid.UUID `json:"account_a"`
	AccountB uuid.UUID `json:"account_b"`
	First    uuid.UUID `json:"first"`
	Last     uuid.UUID `json:"last"`
}

// checkVerdict is the classify step's answer, codes only.
type checkVerdict struct {
	Refused bool           `json:"refused"`
	Verdict domain.Verdict `json:"verdict"`
}

// checkFacts are the fact signals of each side that has a message code.
type checkFacts struct {
	A []domain.Signal `json:"a"`
	B []domain.Signal `json:"b"`
}

// window reads conversation's window up to last and redacts it. Each line
// carries its sender's side.
func (s ScamCheck) window(rc restate.RunContext, conversation, last uuid.UUID) (domain.Window, error) {
	w, _, err := s.Chats.Window(rc, conversation, last, domain.WindowSize)
	if err != nil {
		return domain.Window{}, fmt.Errorf("read the window: %w", err)
	}
	out := domain.Window{Lines: make([]domain.Line, 0, len(w.Lines))}
	for _, l := range w.Lines {
		side := domain.SideA
		if l.SenderID == w.AccountB {
			side = domain.SideB
		}
		out.Lines = append(out.Lines, domain.Line{MessageID: l.ID, Side: side, Text: domain.Redact(l.Text), ListingID: l.ListingID})
	}
	return out, nil
}

// Run plans, classifies, gathers facts and saves, each in a step. No step
// returns message text. A refusal, or a classify step that runs out of
// retries, skips the check; nothing here fails a message.
func (s ScamCheck) Run(ctx restate.ObjectContext) error {
	conversation, err := uuid.Parse(restate.Key(ctx))
	if err != nil {
		//nolint:wrapcheck // Restate must receive the terminal error itself
		return restate.TerminalErrorf("conversation key %q: %v", restate.Key(ctx), err)
	}
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
	}
	plan, err := restate.Run(ctx, func(rc restate.RunContext) (checkPlan, error) {
		w, found, stepErr := s.Chats.Window(rc, conversation, uuid.UUID{}, domain.WindowSize)
		if stepErr != nil {
			return checkPlan{}, fmt.Errorf("read the window: %w", stepErr)
		}
		if !found || len(w.Lines) == 0 {
			return checkPlan{}, nil
		}
		newest := w.Lines[len(w.Lines)-1].ID
		last, count, stepErr := s.Scams.Checked(rc, conversation, at.Add(-24*time.Hour))
		if stepErr != nil {
			return checkPlan{}, fmt.Errorf("read the checks: %w", stepErr)
		}
		if last == newest {
			return checkPlan{}, nil
		}
		if count >= domain.ChecksPerDay {
			s.Log.InfoContext(rc, "scam checks capped for the day", "conversation", conversation, "checks", count)
			return checkPlan{}, nil
		}
		return checkPlan{Run: true, AccountA: w.AccountA, AccountB: w.AccountB, First: w.Lines[0].ID, Last: newest}, nil
	})
	if err != nil {
		return fmt.Errorf("plan the check: %w", err)
	}
	if !plan.Run {
		return nil
	}

	verdict, err := restate.Run(ctx, func(rc restate.RunContext) (checkVerdict, error) {
		w, stepErr := s.window(rc, conversation, plan.Last)
		if stepErr != nil {
			return checkVerdict{}, stepErr
		}
		v, stepErr := s.Classifier.Classify(rc, w)
		if errors.Is(stepErr, ErrRefused) {
			s.Log.ErrorContext(rc, "the classifier refused a scam check", "conversation", conversation, "err", stepErr)
			return checkVerdict{Refused: true}, nil
		}
		if stepErr != nil {
			return checkVerdict{}, fmt.Errorf("classify: %w", stepErr)
		}
		v, dropped := v.Clean()
		if dropped > 0 {
			s.Log.WarnContext(rc, "the classifier gave codes outside the list", "conversation", conversation, "dropped", dropped)
		}
		return checkVerdict{Verdict: v}, nil
	}, restate.WithMaxRetryDuration(classifyRetry), restate.WithMaxRetryAttempts(classifyAttempts))
	if err != nil {
		s.Log.ErrorContext(ctx, "a scam check gave up", "conversation", conversation, "err", err)
		return nil
	}
	if verdict.Refused {
		return nil
	}

	v := verdict.Verdict
	facts, err := restate.Run(ctx, func(rc restate.RunContext) (checkFacts, error) {
		var out checkFacts
		if len(v.A) == 0 && len(v.B) == 0 {
			return out, nil
		}
		w, stepErr := s.window(rc, conversation, plan.Last)
		if stepErr != nil {
			return checkFacts{}, stepErr
		}
		for _, side := range []struct {
			codes   []domain.Code
			suspect uuid.UUID
			into    *[]domain.Signal
		}{{v.A, plan.AccountA, &out.A}, {v.B, plan.AccountB, &out.B}} {
			if len(side.codes) == 0 {
				continue
			}
			for _, f := range s.Facts {
				signals, factErr := f.Signals(rc, side.suspect, w, at)
				if factErr != nil {
					s.Log.WarnContext(rc, "a fact source failed; its signals are left out", "conversation", conversation, "err", factErr)
					continue
				}
				*side.into = append(*side.into, signals...)
			}
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("gather facts: %w", err)
	}

	checkID, writeA, writeB := restatex.NewID(ctx, at), restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		var writes []repository.WarningWrite
		for _, side := range []struct {
			codes           []domain.Code
			facts           []domain.Signal
			suspect, warned uuid.UUID
			writeID         uuid.UUID
		}{
			{v.A, facts.A, plan.AccountA, plan.AccountB, writeA},
			{v.B, facts.B, plan.AccountB, plan.AccountA, writeB},
		} {
			if len(side.codes) == 0 {
				continue
			}
			w, getErr := s.Scams.Get(rc, conversation, side.warned)
			if errors.Is(getErr, repository.ErrNotFound) {
				w, getErr = domain.NewWarning(conversation, side.warned, side.suspect), nil
			}
			if getErr != nil {
				return fmt.Errorf("read the warning: %w", getErr)
			}
			if w.Raise(side.codes, side.facts, at) {
				writes = append(writes, repository.WarningWrite{Warning: &w, WriteID: side.writeID})
			}
		}
		return s.Scams.Record(rc, domain.Check{
			ID: checkID, ConversationID: conversation, FirstMessageID: plan.First, LastMessageID: plan.Last,
			Classifier: s.Classifier.Name(), PromptVersion: s.Classifier.PromptVersion(), A: v.A, B: v.B, CreatedAt: at,
		}, writes, at.Add(-domain.CheckRetention))
	}); err != nil {
		return fmt.Errorf("save the check: %w", err)
	}
	return nil
}
