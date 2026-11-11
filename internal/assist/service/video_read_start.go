package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"
)

// StartVideoRead keeps the phone's transcript under the account and the key
// outside Restate, runs VideoRead.Start once per account and
// idempotencyKey, then reads the read it started. The invocation's key
// holds the account, so the same key sent by two accounts starts two reads.
func (a *Assist) StartVideoRead(ctx context.Context, account uuid.UUID, idempotencyKey string, video uuid.UUID, transcript string,
) (ListingReadView, error) {
	if t := strings.TrimSpace(transcript); t != "" {
		if err := a.d.Reads.PutTranscript(ctx, account, idempotencyKey, t, a.d.Now()); err != nil {
			return ListingReadView{}, fmt.Errorf("keep the transcript: %w", err)
		}
	}
	res, err := a.startVideo.Call(ctx, account.String()+"/"+idempotencyKey,
		StartInput{Account: account, Video: video, RequestKey: idempotencyKey})
	// The insert took the transcript in, or nothing will: a retry answered
	// with the first read, or a refused start. A transient failure that loses
	// it leaves the read to hear the video's sound.
	if delErr := a.d.Reads.DeleteTranscript(ctx, account, idempotencyKey); delErr != nil {
		return ListingReadView{}, errors.Join(err, fmt.Errorf("drop the waiting transcript: %w", delErr))
	}
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
