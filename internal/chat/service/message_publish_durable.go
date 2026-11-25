package service

import (
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// messagePublish is the MessagePublish service's name without the prefix.
const messagePublish = "MessagePublish"

// MessagePublish waits for a photo message's uploads to be processed, then
// sends the message, or fails it with the reason.
type MessagePublish struct{ Chat *Chat }

func (s MessagePublish) ServiceName() string { return s.Chat.d.Prefix + messagePublish }

// loadedMessage is what the load step found.
type loadedMessage struct {
	Message domain.MessageState `json:"message"`
	Found   bool                `json:"found"`
}

// Run processes the message's uploads through MediaProcess with the upload
// ID as idempotency key. A message gone or no longer processing is left as
// it is.
func (s MessagePublish) Run(ctx restate.Context, id uuid.UUID) error {
	ld, err := restate.Run(ctx, func(rc restate.RunContext) (loadedMessage, error) {
		m, stepErr := s.Chat.d.Chats.GetMessage(rc, id)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedMessage{}, nil
		}
		if stepErr != nil {
			return loadedMessage{}, fmt.Errorf("get message: %w", stepErr)
		}
		return loadedMessage{Message: m.State(), Found: true}, nil
	})
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	if !ld.Found || ld.Message.Status != domain.StatusProcessing {
		return nil
	}
	m := domain.RestoreMessage(ld.Message)
	uploads := m.UploadIDs()
	results, procErr := mediasvc.ProcessAll(ctx, s.Chat.d.Prefix, uploads)
	if procErr != nil {
		return fmt.Errorf("process uploads: %w", procErr)
	}
	keys := make(map[uuid.UUID]domain.Photo, len(uploads))
	failure := ""
	for _, u := range uploads {
		if r := results[u]; r.Status == mediadomain.StatusReady {
			keys[u] = domain.Photo{PhotoKey: r.PhotoKey, ThumbKey: r.ThumbKey}
		} else if failure == "" {
			failure = r.Failure
		}
	}
	if failure != "" {
		m.Failed(failure)
	} else {
		m.Published(keys)
	}
	// A message published already, by a retry, or gone meanwhile stays as it is.
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return restatex.Ignore(s.Chat.d.Chats.SaveMessage(rc, m), repository.ErrNotFound)
	}); err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}
