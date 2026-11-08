package events

import (
	"sync"
	"uuid"

	restate "github.com/restatedev/sdk-go"
)

// Inbox is a subscriber that counts how often it handled each event. It is
// exported for the events_test package; the internal tests use it too.
type Inbox struct {
	name string // with the prefix
	mu   *sync.Mutex
	got  map[uuid.UUID]int
}

func NewInbox(prefix, name string) Inbox {
	return Inbox{name: prefix + name, mu: &sync.Mutex{}, got: map[uuid.UUID]int{}}
}

func (i Inbox) ServiceName() string { return i.name }

func (i Inbox) Handle(ctx restate.Context, e Event) error {
	return restate.RunVoid(ctx, func(restate.RunContext) error {
		i.mu.Lock()
		defer i.mu.Unlock()
		i.got[e.ID]++
		return nil
	})
}

// Count is how often the inbox handled id. Restate.Reflect skips it, since
// it takes no restate.Context.
func (i Inbox) Count(id uuid.UUID) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.got[id]
}
