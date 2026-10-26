package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/media/domain"
)

const (
	// putTTL is how long a presigned PUT stays valid.
	putTTL = 15 * time.Minute
	// expireAfter is how long an upload may wait for an attach.
	expireAfter = 24 * time.Hour
)

// CreateUpload starts an upload and returns a presigned PUT for it. The
// expiry is scheduled with Restate before the row exists, so no upload is
// left without one; an expiry that finds no row does nothing.
func (m *Media) CreateUpload(ctx context.Context, owner uuid.UUID, contentType string, size int64) (UploadTicket, error) {
	now := m.now()
	u, err := domain.NewUpload(uuid.NewV7(), owner, contentType, size, now)
	if err != nil {
		return UploadTicket{}, err
	}
	if _, err = ingress.ServiceSend[uuid.UUID](m.ingress, m.prefix+mediaExpire, "Run").
		Send(ctx, u.ID(), restate.WithDelay(expireAfter), restate.WithIdempotencyKey(u.ID().String())); err != nil {
		return UploadTicket{}, fmt.Errorf("schedule the upload's expiry: %w", err)
	}
	if err = m.uploads.Create(ctx, u); err != nil {
		return UploadTicket{}, fmt.Errorf("create upload: %w", err)
	}
	url, headers, err := m.store.PresignPut(ctx, u.Key(), contentType, size, putTTL)
	if err != nil {
		return UploadTicket{}, fmt.Errorf("presign the upload: %w", err)
	}
	return UploadTicket{UploadID: u.ID(), URL: url, Headers: headers, ExpiresAt: now.Add(putTTL)}, nil
}
