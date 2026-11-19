package seedpack

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
	"uuid"
)

// ID is a row's deterministic ID: a UUIDv7 at time at whose random bits
// hash the pack, kind and key, so reloads and wipe find the same rows and
// lists ordered by ID keep the pack's dates.
func ID(pack, kind, key string, at time.Time) uuid.UUID {
	h := sha256.Sum256([]byte(pack + "\x00" + kind + "\x00" + key))
	var u uuid.UUID
	copy(u[:], h[:16])
	ms := uint64(max(at.UnixMilli(), 0))
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(u[:6], ts[2:])
	u[6] = 0x70 | u[6]&0x0F // version 7
	u[8] = 0x80 | u[8]&0x3F // RFC 9562 variant
	return u
}

// AccountID and ListingID are the IDs a load gives a pack's person and
// listing; devseed and other tools find the rows by them.
func (m Manifest) AccountID(a *Account) uuid.UUID { return ID(m.Name, "account", a.Key, a.JoinedAt) }

func (m Manifest) ListingID(l *Listing) uuid.UUID { return ID(m.Name, "listing", l.Key, l.CreatedAt) }
