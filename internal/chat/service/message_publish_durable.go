package service

import (
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
)

// messagePublish is the MessagePublish service's name without the prefix.
const messagePublish = "MessagePublish"

// MessagePublish waits for a photo message's uploads to be processed, then
// sends the message, or fails it with the reason.
type MessagePublish struct{ Chat *Chat }

func (s MessagePublish) ServiceName() string { return s.Chat.prefix + messagePublish }

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
		m, stepErr := s.Chat.chats.GetMessage(rc, id)
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
	futures := make([]restate.ResponseFuture[mediasvc.ProcessResult], len(uploads))
	for i, u := range uploads {
		futures[i] = restate.Service[mediasvc.ProcessResult](ctx, s.Chat.prefix+mediasvc.MediaProcessService, "Run").
			RequestFuture(u, restate.WithIdempotencyKey(u.String()))
	}
	keys := make(map[uuid.UUID]domain.Photo, len(uploads))
	failure := ""
	for i, f := range futures {
		r, waitErr := f.Response()
		if waitErr != nil {
			return fmt.Errorf("process upload %s: %w", uploads[i], waitErr)
		}
		if r.Status == mediadomain.StatusReady {
			keys[uploads[i]] = domain.Photo{PhotoKey: r.PhotoKey, ThumbKey: r.ThumbKey}
		} else if failure == "" {
			failure = r.Failure
		}
	}
	if failure != "" {
		m.Failed(failure)
	} else {
		m.Published(keys)
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Chat.chats.SaveMessage(rc, m) }); err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}
