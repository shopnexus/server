package service

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
)

const (
	// WatchFor is how long a watch lasts at most: a read runs Deadline
	// before Run fails it.
	WatchFor = domain.Deadline + 30*time.Second
	// PingEvery is how often a watch sends a ping, so proxies keep it open.
	PingEvery = 15 * time.Second
)

// ReadEvent is one thing a watch sends: the read, its text so far, or a
// ping.
type ReadEvent struct {
	Read *ListingReadView
	Text *domain.Text
	Ping bool
}

func (a *Assist) WatchRead(ctx context.Context, account, id uuid.UUID, emit func(ReadEvent) error) error {
	// Subscribe before the first read, so an end between them is heard.
	sub := a.d.Relay.Subscribe(id)
	defer sub.Close()
	ended := func() (bool, error) {
		v, err := a.GetRead(ctx, account, id)
		if err != nil {
			return false, err
		}
		if err = emit(ReadEvent{Read: &v}); err != nil {
			return false, err
		}
		return v.Status != domain.StatusReading, nil
	}
	if done, err := ended(); done || err != nil {
		return err
	}
	until := time.NewTimer(WatchFor)
	defer until.Stop()
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.Closed():
			return nil
		case <-until.C:
			_, err := ended()
			return err
		case <-ping.C:
			if err := emit(ReadEvent{Ping: true}); err != nil {
				return err
			}
		case msg := <-sub.C():
			var m relayed
			if msg != nil && json.Unmarshal(msg, &m) == nil && m.Kind == relayText && m.Text != nil {
				if err := emit(ReadEvent{Text: m.Text}); err != nil {
					return err
				}
				continue
			}
			// The end, or messages lost: the stored read tells.
			if done, err := ended(); done || err != nil {
				return err
			}
		}
	}
}
