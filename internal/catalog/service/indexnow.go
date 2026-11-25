package service

import (
	"context"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/provider/indexnow"
)

// IndexNowEvery is how long a changed listing waits in the IndexNow queue,
// so a burst of changes goes in one submission.
const IndexNowEvery = time.Minute

// IndexNowSettings say how catalog tells search engines of changed
// listing pages: the key, the website's origin the pages are under, the
// client that submits them, and how long a listing waits in the queue,
// IndexNowEvery but in tests.
type IndexNowSettings struct {
	Key    string
	Site   string // https://host, without a trailing slash
	Client *indexnow.Client
	Every  time.Duration
}

// On reports whether listing changes are submitted: a key, a client, and
// a site on https, the only scheme IndexNow takes.
func (s IndexNowSettings) On() bool {
	return s.Key != "" && s.Client != nil && strings.HasPrefix(s.Site, "https://")
}

func (c *Catalog) IndexNowKey(_ context.Context) (string, error) {
	if c.d.IndexNow.Key == "" {
		return "", domain.ErrIndexNowOff
	}
	return c.d.IndexNow.Key, nil
}
