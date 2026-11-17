package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopnexus/server2/internal/agent/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/authn"
)

const (
	// readTimeout bounds a call that reads or starts a conversation.
	readTimeout = 10 * time.Second
	// turnTimeout bounds a turn: the agent's own deadline of 60 seconds
	// (NFR-PERF-02) and a margin for the answer to come back.
	turnTimeout = 75 * time.Second
	// maxAnswer is the most of an agent's answer the server reads, and
	// maxRefusal of an error's body.
	maxAnswer  = 4 << 20
	maxRefusal = 4 << 10
)

// agentErrors maps the agent's error codes, its body {code}, to what a
// member sees. A code it lacks (unauthorized, bad_request, internal) means
// the two sides disagree or the agent broke: domain.ErrUnavailable, with
// the code as the cause for the log.
var agentErrors = map[string]*apperr.Error{
	"not_found":           domain.ErrConversationNotFound,
	"agent_budget":        domain.ErrBudget,
	"rate_limited":        domain.ErrRateLimited,
	"agent_timeout":       domain.ErrTimeout,
	"agent_unavailable":   domain.ErrUnavailable,
	"input_rejected":      domain.ErrInputRejected,
	"turn_in_progress":    domain.ErrTurnInProgress,
	"conversation_closed": domain.ErrConversationClosed,
}

// call sends one request to the agent service for caller, with the service
// token and the member's id and role, and decodes a 2xx answer into out. A
// call past timeout is late; an error answer becomes the error agentErrors
// gives its code, or for a body without one the error of its status (404,
// 504). No body is logged, since a turn quotes people.
func (s *Agent) call(ctx context.Context, caller authn.ConsoleMember, method, path string, query url.Values, in, out any,
	timeout time.Duration, late *apperr.Error,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode the agent request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	target := strings.TrimSuffix(s.baseURL, "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("build the agent request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("X-Console-Member", caller.MemberID.String())
	req.Header.Set("X-Console-Role", caller.Role)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.http.Do(req)
	if errors.Is(err, context.DeadlineExceeded) {
		return late.WithCause(err)
	}
	if err != nil {
		return domain.ErrUnavailable.WithCause(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, maxRefusal)).Decode(&refusal)
		if e, ok := agentErrors[refusal.Code]; ok {
			return e
		}
		switch res.StatusCode {
		case http.StatusNotFound:
			return domain.ErrConversationNotFound
		case http.StatusGatewayTimeout:
			return domain.ErrTimeout
		default:
			return domain.ErrUnavailable.WithCause(fmt.Errorf("%s %s answered %d %q", method, path, res.StatusCode, refusal.Code))
		}
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxAnswer)).Decode(out); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return late.WithCause(err)
		}
		return domain.ErrUnavailable.WithCause(fmt.Errorf("read the answer to %s %s: %w", method, path, err))
	}
	return nil
}
