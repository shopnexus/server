// Package push sends notifications to phones. It knows nothing about what
// they say; callers write the title and the body.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Message is one push: what the notification shows and the data the app
// opens it with. A push with the same Tag replaces an earlier one.
type Message struct {
	Title, Body string
	Tag         string
	Data        map[string]string
}

// Sender delivers a push to one device token.
type Sender interface {
	Send(ctx context.Context, token string, m Message) error
}

var (
	// ErrDeadToken is a token FCM no longer delivers to; the caller deletes
	// it.
	ErrDeadToken = errors.New("push token is no longer valid")
	// ErrRetry is a failure worth retrying: FCM throttled or was down.
	ErrRetry = errors.New("push failed, retry")
)

// LogSender writes pushes to the log instead of sending them. Development
// only. It logs the title and tag and the token's end, never the body.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, token string, m Message) error {
	end := token
	if len(end) > 6 {
		end = end[len(end)-6:]
	}
	s.Log.InfoContext(ctx, "push (dev sender)", "title", m.Title, "tag", m.Tag, "token_end", end)
	return nil
}

// fcmEndpoint is FCM HTTP v1's base URL.
const fcmEndpoint = "https://fcm.googleapis.com"

// FCM sends through Firebase Cloud Messaging's HTTP v1 API.
type FCM struct {
	client   *http.Client
	endpoint string
	project  string
}

// NewFCM signs requests with the service account in credentialsJSON.
func NewFCM(ctx context.Context, credentialsJSON []byte, projectID string) (*FCM, error) {
	// The type check refuses any credential other than a service account.
	creds, err := google.CredentialsFromJSONWithType(ctx, credentialsJSON, google.ServiceAccount,
		"https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("read FCM credentials: %w", err)
	}
	return NewFCMWith(oauth2.NewClient(ctx, creds.TokenSource), fcmEndpoint, projectID), nil
}

// NewFCMWith sends through client to endpoint; NewFCM passes a signing
// client and FCM's own endpoint, tests a local server.
func NewFCMWith(client *http.Client, endpoint, projectID string) *FCM {
	return &FCM{client: client, endpoint: endpoint, project: projectID}
}

// maxReply caps how much of an error reply is read.
const maxReply = 64 << 10

func (f *FCM) Send(ctx context.Context, token string, m Message) error {
	body, err := json.Marshal(map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"android":      map[string]any{"collapse_key": m.Tag, "notification": map[string]string{"tag": m.Tag}},
		"data":         m.Data,
	}})
	if err != nil {
		return fmt.Errorf("encode push: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		f.endpoint+"/v1/projects/"+f.project+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("send push: %w: %w", ErrRetry, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var reply struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"` //nolint:tagliatelle // FCM's field name
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&reply)
	for _, d := range reply.Error.Details {
		if d.ErrorCode == "UNREGISTERED" {
			return ErrDeadToken
		}
	}
	switch {
	case reply.Error.Status == "INVALID_ARGUMENT":
		return ErrDeadToken
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("FCM answered %d: %w", res.StatusCode, ErrRetry)
	default:
		return fmt.Errorf("FCM answered %d %s", res.StatusCode, reply.Error.Status)
	}
}
