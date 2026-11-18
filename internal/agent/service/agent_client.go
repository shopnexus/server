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

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

const (
	// readTimeout bounds every call but a turn's event stream: the agent
	// answers a question with the turn at once and runs it on a worker.
	readTimeout = 10 * time.Second
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
	"agent_budget":        domain.ErrBudget,
	"rate_limited":        domain.ErrRateLimited,
	"agent_timeout":       domain.ErrTimeout,
	"agent_unavailable":   domain.ErrUnavailable,
	"input_rejected":      domain.ErrInputRejected,
	"turn_in_progress":    domain.ErrTurnInProgress,
	"conversation_closed": domain.ErrConversationClosed,
	"turn_state":          domain.ErrTurnState,
	"skill_exists":        domain.ErrSkillTaken,
	"forbidden":           domain.ErrForbidden,
}

// call sends one request to the agent service for caller, with the service
// token and the member's id and role, and decodes a 2xx answer into out. A
// call past timeout is late; an error answer becomes the error agentErrors
// gives its code, or for a body without one the error of its status (404,
// 504). No body is logged, since a turn quotes people.
func (s *Agent) call(ctx context.Context, caller authn.ConsoleMember, method, path string, query url.Values, in, out any) error {
	return s.send(ctx, caller, request{
		method: method, path: path, query: query, in: in, out: out, timeout: readTimeout, late: domain.ErrUnavailable,
		notFound: domain.ErrConversationNotFound,
	})
}

// request is one call to the agent service. notFound is the error of a
// 404 without a code agentErrors maps, and of the code not_found;
// badRequest, when set, is the error of bad_request, which is otherwise a
// disagreement between the two sides.
type request struct {
	method, path string
	query        url.Values
	in, out      any
	timeout      time.Duration
	late         *apperr.Error
	notFound     *apperr.Error
	badRequest   *apperr.Error
}

// send is call with the 404 of the thing the path names.
func (s *Agent) send(ctx context.Context, caller authn.ConsoleMember, rq request) error {
	method, path, query, in, out, late := rq.method, rq.path, rq.query, rq.in, rq.out, rq.late
	ctx, cancel := context.WithTimeout(ctx, rq.timeout)
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
	req, err := s.newRequest(ctx, caller, method, target, body)
	if err != nil {
		return err
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
		return refusalOf(res, method, path, rq.notFound, rq.badRequest)
	}
	if out == nil {
		return nil
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxAnswer)).Decode(out); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return late.WithCause(err)
		}
		return domain.ErrUnavailable.WithCause(fmt.Errorf("read the answer to %s %s: %w", method, path, err))
	}
	return nil
}

// newRequest is a request to the agent with the service token and the
// member's id and role.
func (s *Agent) newRequest(ctx context.Context, caller authn.ConsoleMember, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build the agent request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("X-Console-Member", caller.MemberID.String())
	req.Header.Set("X-Console-Role", caller.Role)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// refusalOf is the error of an answer outside 2xx: the one agentErrors
// gives its code, notFound for not_found or a bare 404, agent_timeout for a
// bare 504, else agent_unavailable with the code as the cause for the log.
func refusalOf(res *http.Response, method, path string, notFound, badRequest *apperr.Error) error {
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, maxRefusal)).Decode(&refusal)
	if refusal.Code == "not_found" {
		return notFound
	}
	if refusal.Code == "bad_request" && badRequest != nil {
		return badRequest
	}
	if e, ok := agentErrors[refusal.Code]; ok {
		return e
	}
	switch res.StatusCode {
	case http.StatusNotFound:
		return notFound
	case http.StatusGatewayTimeout:
		return domain.ErrTimeout
	default:
		return domain.ErrUnavailable.WithCause(fmt.Errorf("%s %s answered %d %q", method, path, res.StatusCode, refusal.Code))
	}
}
