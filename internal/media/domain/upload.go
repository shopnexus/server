// Package domain holds module media's entity: an upload of one photo, from
// the presigned PUT to the processed photo and thumbnail.
package domain

import (
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

const (
	// MaxPhotoBytes is the largest photo upload, MaxVideoBytes the largest
	// video.
	MaxPhotoBytes int64 = 15 << 20
	MaxVideoBytes int64 = 50 << 20

	ContentTypeMP4       = "video/mp4"
	ContentTypeQuickTime = "video/quicktime"
)

var ErrInvalidUpload = apperr.New(apperr.KindInvalid, "invalid_upload",
	"an upload must be the caller's, waiting, put to the store, and a JPEG, PNG or WebP of at most 15 MB "+
		"or an MP4 or QuickTime video of at most 50 MB")

var contentTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, ContentTypeMP4: true, ContentTypeQuickTime: true,
}

// IsVideo reports whether contentType is one of the video types an upload
// may have.
func IsVideo(contentType string) bool {
	return contentType == ContentTypeMP4 || contentType == ContentTypeQuickTime
}

// Status is where an upload is: waiting for its PUT and an attach, attached
// to a listing and being processed, ready or failed when processed, expired
// when nothing attached it in time, consumed when it was a video and the
// read that held it ended, or released when its holder no longer shows it.
type Status string

const (
	StatusWaiting  Status = "waiting"
	StatusAttached Status = "attached"
	StatusReady    Status = "ready"
	StatusFailed   Status = "failed"
	StatusExpired  Status = "expired"
	StatusConsumed Status = "consumed"
	StatusReleased Status = "released"
)

// Shape is what processing makes of an upload: a photo at most 1600 px on
// its long side with a thumbnail, or a square of at most 400 px, such as an
// avatar.
type Shape string

const (
	ShapePhoto  Shape = "photo"
	ShapeSquare Shape = "square"
	// ShapeQuery is a search's photo: at most 768 px on its long side, never stored.
	ShapeQuery Shape = "query"
	// ShapeVideo is a video a read holds; it is never processed, and its
	// object is deleted when the read ends.
	ShapeVideo Shape = "video"
)

// Origin is where an upload came from. Only the server makes frames and
// copies, and only photos.
type Origin string

const (
	OriginApp   Origin = "app"   // the app put it
	OriginFrame Origin = "frame" // the server cut it from the seller's video
	OriginLink  Origin = "link"  // the server copied it from the seller's listing on another site
	// OriginListing is a photo the server copied from the seller's own
	// listing, for a new listing.
	OriginListing Origin = "listing"
)

// Upload is one photo or video a client puts to uploads/<id>.
type Upload struct {
	s UploadState
}

func NewUpload(id, owner uuid.UUID, contentType string, size int64, origin Origin, now time.Time) (Upload, error) {
	limit := MaxPhotoBytes
	if IsVideo(contentType) {
		limit = MaxVideoBytes
	}
	if !contentTypes[contentType] || size < 1 || size > limit {
		return Upload{}, ErrInvalidUpload
	}
	switch origin {
	case OriginApp:
	case OriginFrame, OriginLink, OriginListing:
		if IsVideo(contentType) {
			return Upload{}, ErrInvalidUpload
		}
	default:
		return Upload{}, ErrInvalidUpload
	}
	return Upload{s: UploadState{
		ID: id, OwnerID: owner, ContentType: contentType, Size: size, Origin: origin, Status: StatusWaiting,
		WriteID: id, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(ExpireAfter),
	}}, nil
}

func (u *Upload) ID() uuid.UUID       { return u.s.ID }
func (u *Upload) OwnerID() uuid.UUID  { return u.s.OwnerID }
func (u *Upload) HolderID() uuid.UUID { return u.s.HolderID }
func (u *Upload) Shape() Shape        { return u.s.Shape }
func (u *Upload) ContentType() string { return u.s.ContentType }
func (u *Upload) Size() int64         { return u.s.Size }
func (u *Upload) Origin() Origin      { return u.s.Origin }
func (u *Upload) Status() Status      { return u.s.Status }
func (u *Upload) PhotoKey() string    { return u.s.PhotoKey }
func (u *Upload) ThumbKey() string    { return u.s.ThumbKey }
func (u *Upload) Failure() string     { return u.s.Failure }
func (u *Upload) WriteID() uuid.UUID  { return u.s.WriteID }

// UploadKey is where the client puts the original of upload id.
func UploadKey(id uuid.UUID) string { return "uploads/" + id.String() }

// Key is where the client puts the original.
func (u *Upload) Key() string { return UploadKey(u.s.ID) }

// PublicKeys are where processing puts upload id's photo, or square, and
// thumbnail under the public media/.
func PublicKeys(id uuid.UUID) (photo, thumb string) {
	return "media/" + id.String() + ".jpg", "media/" + id.String() + "_thumb.jpg"
}

// Attach gives a waiting upload of owner to holder, such as a listing, to
// be processed into shape, and reports whether it changed the upload. A
// video goes only in shape video and a photo never does; a video past its
// expiry is refused even while it still waits. An upload holder already
// holds in that shape stays as it is, so a retried attach succeeds; an
// expired, consumed or released upload is never held again.
func (u *Upload) Attach(owner, holder uuid.UUID, shape Shape, now time.Time) (bool, error) {
	if owner != u.s.OwnerID || IsVideo(u.s.ContentType) != (shape == ShapeVideo) {
		return false, ErrInvalidUpload
	}
	if u.s.Status == StatusWaiting {
		if shape == ShapeVideo && !now.Before(u.ExpiresAt()) {
			return false, ErrInvalidUpload
		}
		u.s.Status, u.s.HolderID, u.s.Shape, u.s.UpdatedAt = StatusAttached, holder, shape, now
		return true, nil
	}
	if u.s.Status != StatusExpired && u.s.Status != StatusConsumed && u.s.Status != StatusReleased &&
		u.s.HolderID == holder && u.s.Shape == shape {
		return false, nil
	}
	return false, ErrInvalidUpload
}

// Processed records the processed photo and thumbnail.
func (u *Upload) Processed(photoKey, thumbKey string, now time.Time) {
	u.s.Status, u.s.PhotoKey, u.s.ThumbKey, u.s.UpdatedAt = StatusReady, photoKey, thumbKey, now
}

// Failed records why processing refused the photo.
func (u *Upload) Failed(reason string, now time.Time) {
	u.s.Status, u.s.Failure, u.s.UpdatedAt = StatusFailed, reason, now
}

// Expire ends an upload that is still waiting at its expiry and reports
// whether it did; an upload before its expiry, or attached, processed or
// expired, stays as it is.
func (u *Upload) Expire(now time.Time) bool {
	if u.s.Status != StatusWaiting || now.Before(u.s.ExpiresAt) {
		return false
	}
	u.s.Status, u.s.UpdatedAt = StatusExpired, now
	return true
}

// Consume ends a held video once the read that held it is over, and reports
// whether it did; any other upload, or a video already consumed, stays as it
// is.
func (u *Upload) Consume(now time.Time) bool {
	if u.s.Shape != ShapeVideo || u.s.Status != StatusAttached {
		return false
	}
	u.s.Status, u.s.UpdatedAt = StatusConsumed, now
	return true
}

// Release ends a photo holder holds once holder no longer shows it, and
// reports whether it did; a video, another holder's upload or one already
// ended stays as it is.
func (u *Upload) Release(holder uuid.UUID, now time.Time) bool {
	if u.s.HolderID != holder || u.s.Shape == ShapeVideo ||
		(u.s.Status != StatusAttached && u.s.Status != StatusReady && u.s.Status != StatusFailed) {
		return false
	}
	u.s.Status, u.s.UpdatedAt = StatusReleased, now
	return true
}

// Extend keeps a waiting photo of owner's for their drafts: its expiry
// moves to ExtendFor from now, unless it is later already. Nothing brings
// it in. Another owner's upload, a video, or a photo that is not waiting
// before its expiry is refused.
func (u *Upload) Extend(owner uuid.UUID, now time.Time) error {
	if owner != u.s.OwnerID || IsVideo(u.s.ContentType) || u.s.Status != StatusWaiting || !now.Before(u.s.ExpiresAt) {
		return ErrInvalidUpload
	}
	if end := now.Add(ExtendFor); u.s.ExpiresAt.Before(end) {
		u.s.ExpiresAt, u.s.UpdatedAt = end, now
	}
	return nil
}

// Gone reports whether an upload of owner's can no longer be in their
// drafts: a holder took it, it expired, or it waits past its expiry.
// Another owner's upload is not gone; Extend refuses it.
func (u *Upload) Gone(owner uuid.UUID, now time.Time) bool {
	if owner != u.s.OwnerID {
		return false
	}
	if u.s.Status == StatusWaiting {
		return !now.Before(u.s.ExpiresAt)
	}
	return true // attached, ready, failed, expired, consumed, released
}

// UploadState is every stored field of an upload. Stores only.
type UploadState struct {
	ID          uuid.UUID `json:"id"`
	OwnerID     uuid.UUID `json:"owner_id"`
	HolderID    uuid.UUID `json:"holder_id"` // what the upload is attached to; zero while waiting
	Shape       Shape     `json:"shape"`     // set with the holder
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Origin      Origin    `json:"origin"`
	Status      Status    `json:"status"`
	PhotoKey    string    `json:"photo_key"`
	ThumbKey    string    `json:"thumb_key"`
	Failure     string    `json:"failure"`
	ExpiresAt   time.Time `json:"expires_at"` // when a waiting upload is deleted; a draft moves it out
	WriteID     uuid.UUID `json:"write_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RestoreUpload rebuilds a stored upload. Stores only.
func RestoreUpload(st UploadState) Upload { return Upload{s: st} }

// State returns every stored field. Stores only.
func (u *Upload) State() UploadState { return u.s }

// Saved records that a store wrote u under writeID. Stores only.
func (u *Upload) Saved(writeID uuid.UUID) { u.s.WriteID = writeID }

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

// ExpireAfter is how long a new upload may wait for an attach or a draft.
const ExpireAfter = 24 * time.Hour

// ExtendFor is how long a draft keeps the photos it names after each PUT:
// catalog's domain.DraftLife.
const ExtendFor = 30 * 24 * time.Hour

// ExpiresAt is when a waiting upload is deleted: a day after it was
// created, or later once a draft extended it.
func (u *Upload) ExpiresAt() time.Time { return u.s.ExpiresAt }

// Queryable reports whether owner may search with u now: it is theirs, still
// waiting and before its expiry.
func (u *Upload) Queryable(owner uuid.UUID, now time.Time) bool {
	return u.s.OwnerID == owner && u.s.Status == StatusWaiting && now.Before(u.ExpiresAt())
}
