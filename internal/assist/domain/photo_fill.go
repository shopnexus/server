package domain

import "uuid"

// MaxPhotoDescription is the longest description a photo read writes: a
// sentence or two of what the photo shows.
const MaxPhotoDescription = 300

// PhotoDraft is the draft of a photo read's reply, with photo as its one
// photo.
func PhotoDraft(r Reply, photo uuid.UUID) (Draft, error) {
	d, err := textDraft(r, MaxPhotoDescription)
	if err != nil {
		return Draft{}, err
	}
	d.Photos = []uuid.UUID{photo}
	return d, nil
}
