package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"
)

// StartVideoRead is UC-LST-01, step 2: a key the account started a read
// with already answers that read; otherwise it keeps the phone's
// transcript under the account and the key outside Restate, runs
// VideoRead.Start, then reads the read it started. The same key sent by
// two accounts starts two reads.
func (a *Assist) StartVideoRead(ctx context.Context, account, key, video uuid.UUID, transcript string) (ListingReadView, error) {
	if v, ok, err := a.readOfKey(ctx, account, key); ok || err != nil {
		return v, err
	}
	if t := strings.TrimSpace(transcript); t != "" {
		if err := a.d.Reads.PutTranscript(ctx, account, key.String(), t, a.d.Now()); err != nil {
			return ListingReadView{}, fmt.Errorf("keep the transcript: %w", err)
		}
	}
	res, err := a.startVideo.Call(ctx, "", StartInput{Account: account, Video: video, Key: key})
	// The insert took the transcript in, or nothing will: a retry answered
	// with the first read, or a refused start. A transient failure that loses
	// it leaves the read to hear the video's sound.
	if delErr := a.d.Reads.DeleteTranscript(ctx, account, key.String()); delErr != nil {
		return ListingReadView{}, errors.Join(err, fmt.Errorf("drop the waiting transcript: %w", delErr))
	}
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
