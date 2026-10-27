// Package domain holds module media's entity: an upload of one photo, from
// the presigned PUT to the processed photo and thumbnail.
package domain

import (
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// MaxSize is the largest upload, in bytes.
const MaxSize = 15 << 20

var ErrInvalidUpload = apperr.New(apperr.KindInvalid, "invalid_upload",
	"an upload must be the caller's, waiting, put to the store, and a JPEG, PNG or WebP of at most 15 MB")

var contentTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// Status is where an upload is: waiting for its PUT and an attach, attached
// to a listing and being processed, ready or failed when processed, or
// expired when nothing attached it in time.
type Status string

const (
	StatusWaiting  Status = "waiting"
	StatusAttached Status = "attached"
	StatusReady    Status = "ready"
	StatusFailed   Status = "failed"
	StatusExpired  Status = "expired"
)

// Shape is what processing makes of an upload: a photo at most 1600 px on
// its long side with a thumbnail, or a square of at most 400 px, such as an
// avatar.
type Shape string

const (
	ShapePhoto  Shape = "photo"
	ShapeSquare Shape = "square"
)

// Upload is one photo a client puts to uploads/<id>.
type Upload struct {
	id          uuid.UUID
	ownerID     uuid.UUID
	holderID    uuid.UUID // what the upload is attached to; zero while waiting
	shape       Shape     // set with the holder
	contentType string
	size        int64
	status      Status
	photoKey    string
	thumbKey    string
	failure     string
	writeID     uuid.UUID
	createdAt   time.Time
	updatedAt   time.Time
}

// NewUpload starts an upload waiting for its PUT. The first write ID is the
// id.
func NewUpload(id, owner uuid.UUID, contentType string, size int64, now time.Time) (Upload, error) {
	if !contentTypes[contentType] || size < 1 || size > MaxSize {
		return Upload{}, ErrInvalidUpload
	}
	return Upload{
		id: id, ownerID: owner, contentType: contentType, size: size, status: StatusWaiting,
		writeID: id, createdAt: now, updatedAt: now,
	}, nil
}

func (u *Upload) ID() uuid.UUID       { return u.id }
func (u *Upload) OwnerID() uuid.UUID  { return u.ownerID }
func (u *Upload) HolderID() uuid.UUID { return u.holderID }
func (u *Upload) Shape() Shape        { return u.shape }
func (u *Upload) ContentType() string { return u.contentType }
func (u *Upload) Size() int64         { return u.size }
func (u *Upload) Status() Status      { return u.status }
func (u *Upload) PhotoKey() string    { return u.photoKey }
func (u *Upload) ThumbKey() string    { return u.thumbKey }
func (u *Upload) Failure() string     { return u.failure }
func (u *Upload) WriteID() uuid.UUID  { return u.writeID }

// Key is where the client puts the original.
func (u *Upload) Key() string { return "uploads/" + u.id.String() }

// Attach gives a waiting upload of owner to holder, such as a listing, to
// be processed into shape, and reports whether it changed the upload. An
// upload holder already holds in that shape stays as it is, so a retried
// attach succeeds.
func (u *Upload) Attach(owner, holder uuid.UUID, shape Shape, now time.Time) (bool, error) {
	if owner != u.ownerID {
		return false, ErrInvalidUpload
	}
	if u.status == StatusWaiting {
		u.status, u.holderID, u.shape, u.updatedAt = StatusAttached, holder, shape, now
		return true, nil
	}
	if u.status != StatusExpired && u.holderID == holder && u.shape == shape {
		return false, nil
	}
	return false, ErrInvalidUpload
}

// Processed records the processed photo and thumbnail.
func (u *Upload) Processed(photoKey, thumbKey string, now time.Time) {
	u.status, u.photoKey, u.thumbKey, u.updatedAt = StatusReady, photoKey, thumbKey, now
}

// Failed records why processing refused the photo.
func (u *Upload) Failed(reason string, now time.Time) {
	u.status, u.failure, u.updatedAt = StatusFailed, reason, now
}

// Expire ends an upload still waiting and reports whether it did; an
// attached, processed or expired one stays as it is.
func (u *Upload) Expire(now time.Time) bool {
	if u.status != StatusWaiting {
		return false
	}
	u.status, u.updatedAt = StatusExpired, now
	return true
}

// UploadState is every stored field of an upload. Stores only.
type UploadState struct {
	ID          uuid.UUID `json:"id"`
	OwnerID     uuid.UUID `json:"owner_id"`
	HolderID    uuid.UUID `json:"holder_id"`
	Shape       Shape     `json:"shape"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Status      Status    `json:"status"`
	PhotoKey    string    `json:"photo_key"`
	ThumbKey    string    `json:"thumb_key"`
	Failure     string    `json:"failure"`
	WriteID     uuid.UUID `json:"write_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RestoreUpload rebuilds a stored upload. Stores only.
func RestoreUpload(st UploadState) Upload {
	return Upload{
		id: st.ID, ownerID: st.OwnerID, holderID: st.HolderID, shape: st.Shape, contentType: st.ContentType, size: st.Size, status: st.Status,
		photoKey: st.PhotoKey, thumbKey: st.ThumbKey, failure: st.Failure, writeID: st.WriteID,
		createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
}

// State returns every stored field. Stores only.
func (u *Upload) State() UploadState {
	return UploadState{
		ID: u.id, OwnerID: u.ownerID, HolderID: u.holderID, Shape: u.shape, ContentType: u.contentType, Size: u.size, Status: u.status,
		PhotoKey: u.photoKey, ThumbKey: u.thumbKey, Failure: u.failure, WriteID: u.writeID,
		CreatedAt: u.createdAt, UpdatedAt: u.updatedAt,
	}
}

// Saved records that a store wrote u under writeID. Stores only.
func (u *Upload) Saved(writeID uuid.UUID) { u.writeID = writeID }

// MarshalJSON lets a Restate step return an upload for its journal.
func (u Upload) MarshalJSON() ([]byte, error) { return json.Marshal(u.State()) }

func (u *Upload) UnmarshalJSON(b []byte) error {
	var st UploadState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*u = RestoreUpload(st)
	return nil
}
