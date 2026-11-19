package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
)

// agentTurn is what the runs tests read of a turn.
type agentTurn struct {
	Status      string     `json:"status"`
	ErrorCode   string     `json:"error_code"`
	Continuable bool       `json:"continuable"`
	LastSeq     int        `json:"last_seq"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMS  int64      `json:"duration_ms"`
	Sources     []struct {
		N    int    `json:"n"`
		Tool string `json:"tool"`
	} `json:"sources"`
	Attachments []struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"attachments"`
	Steers []struct {
		SteerID string `json:"steer_id"`
	} `json:"steers"`
}

// A turn's run streams back from the agent into the turn, its tool call,
// its document and its events; the controls pause, steer, answer, end and
// continue it, the agent polls pause and steer from the control endpoint,
// and the skills are the server's. UC-MOD-08 A1.
func TestConsoleAgentRuns(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	signedOut := consoleSession{}
	missing := newID()
	const turns = "/v1/console/agent/turns/"
	turnOf := func(id string) agentTurn {
		t.Helper()
		return expect[agentTurn](t, consoleCall(t, lan, "GET", turns+id, nil), 200)
	}

	conversation := expect[agentConversation](t, consoleCall(t, lan, "POST", "/v1/console/agent/conversations", map[string]any{}),
		http.StatusOK)
	check(t, "an unknown field", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "?", "speed": "fast"}), 400, "")
	check(t, "an unknown agent", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "?", "agent_id": newID()}), 404, "ai_agent_not_found")
	mentioned := newID()
	for name, attachment := range map[string]map[string]any{
		"an unknown kind": {"kind": "message", "id": mentioned},
		"a long label":    {"kind": "ticket", "id": mentioned, "label": strings.Repeat("ê", 121)},
	} {
		check(t, name, consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
			map[string]any{"text": "?", "attachments": []map[string]any{attachment}}), 400, "invalid_request")
	}
	started := expect[agentTurnStarted](t, consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "Tuần này thế nào?", "attachments": []map[string]any{
			{"kind": "ticket", "id": mentioned, "label": " Ticket #12 "}, {"kind": "ticket", "id": mentioned},
		}}), http.StatusAccepted)
	ok := started.TurnID
	if events := waitTurn(t, lan, ok); !strings.Contains(events, "event: turn.started") || !strings.Contains(events, `"type":"turn.finished"`) {
		t.Errorf("events %s", events)
	}
	events := consoleCall(t, lan, "GET", turns+ok+"/events", nil, [2]string{"Last-Event-ID", "1"})
	if events.Status != 200 || strings.Contains(string(events.Body), "turn.started") || !strings.Contains(string(events.Body), "id: 2") ||
		events.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("events after 1: %d %s", events.Status, events.Body)
	}
	if after := consoleCall(t, lan, "GET", turns+ok+"/events?after=1", nil); after.Status != 200 ||
		strings.Contains(string(after.Body), "turn.started") {
		t.Errorf("events after=1: %d %s", after.Status, after.Body)
	}
	if view := turnOf(ok); view.Status != "succeeded" || view.LastSeq != 2 || len(view.Sources) != 1 || view.Sources[0].N != 1 ||
		view.FinishedAt == nil || view.DurationMS != 4200 || len(view.Attachments) != 1 || view.Attachments[0].ID != mentioned ||
		view.Attachments[0].Label != "Ticket #12" {
		t.Errorf("turn %+v", view)
	}
	if sent := agentStub.mentionsOf(ok); !slices.Equal(sent, []string{"ticket:" + mentioned}) {
		t.Errorf("the run's mentions %v", sent)
	}
	const list = "/v1/console/agent/conversations"
	if found := expect[struct {
		Items []agentConversation `json:"items"`
	}](t, consoleCall(t, lan, "GET", list+"?mention_kind=ticket&mention_id="+mentioned, nil), 200); len(found.Items) != 1 ||
		found.Items[0].ID != conversation.ID {
		t.Errorf("the conversations that mention the ticket %+v", found)
	}
	for name, query := range map[string]string{
		"a mention kind alone":   "?mention_kind=ticket",
		"a mention id alone":     "?mention_id=" + mentioned,
		"an unknown kind":        "?mention_kind=message&mention_id=" + mentioned,
		"a mention with subject": "?subject_kind=ticket&mention_kind=ticket&mention_id=" + mentioned,
	} {
		check(t, name, consoleCall(t, lan, "GET", list+query, nil), 400, "invalid_request")
	}
	check(t, "an unknown turn", consoleCall(t, lan, "GET", turns+missing, nil), 404, "agent_turn_not_found")
	expectStatus(t, consoleCall(t, signedOut, "GET", turns+ok, nil), 401)
	check(t, "the events of an unknown turn", consoleCall(t, lan, "GET", turns+missing+"/events", nil), 404, "agent_turn_not_found")
	expectStatus(t, consoleCall(t, signedOut, "GET", turns+ok+"/events", nil), 401)

	made := agentStub.madeBy(ok)
	if call := expect[struct {
		Status        string `json:"status"`
		Attempts      int    `json:"attempts"`
		AttemptErrors []any  `json:"attempt_errors"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/agent/tool-calls/"+made[0], nil), 200); call.Status != "success" || call.Attempts != 2 ||
		len(call.AttemptErrors) != 1 {
		t.Errorf("tool call %+v", call)
	}
	if doc := expect[struct {
		LatestVersion int   `json:"latest_version"`
		Versions      []any `json:"versions"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/agent/documents/"+made[1], nil), 200); doc.LatestVersion != 2 ||
		len(doc.Versions) != 2 {
		t.Errorf("document %+v", doc)
	}
	for path, code := range map[string]string{
		"/v1/console/agent/tool-calls/": "agent_tool_call_not_found", "/v1/console/agent/documents/": "agent_document_not_found",
	} {
		check(t, path+" unknown", consoleCall(t, lan, "GET", path+missing, nil), 404, code)
		expectStatus(t, consoleCall(t, signedOut, "GET", path+missing, nil), 401)
	}

	bodies := map[string]any{
		"pause": nil, "stop": nil, "continue": nil, "end": nil,
		"steer": map[string]string{"text": "Chỉ Hà Nội"},
		"input": map[string]any{"answers": map[string]any{"range": "week"}},
	}
	for c, body := range bodies {
		check(t, c+" a finished turn", consoleCall(t, lan, "POST", turns+ok+"/"+c, body), 409, "agent_turn_state")
		check(t, c+" an unknown turn", consoleCall(t, lan, "POST", turns+missing+"/"+c, body), 404, "agent_turn_not_found")
		expectStatus(t, consoleCall(t, signedOut, "POST", turns+ok+"/"+c, body), 401)
	}
	check(t, "a steer without text", consoleCall(t, lan, "POST", turns+ok+"/steer", map[string]any{}), 400, "")
	check(t, "input without answers", consoleCall(t, lan, "POST", turns+ok+"/input", map[string]any{"answers": map[string]any{}}), 400, "")

	// A running turn takes a steer and a pause from the control queue.
	_, held := ask(t, lan, "hold")
	steer := expect[struct {
		SteerID string `json:"steer_id"`
		Resumed bool   `json:"resumed"`
	}](t, consoleCall(t, lan, "POST", turns+held+"/steer", map[string]string{"text": "Chỉ Hà Nội"}), 202)
	expectStatus(t, consoleCall(t, lan, "POST", turns+held+"/pause", nil), 202)
	expectStatus(t, consoleCall(t, lan, "POST", turns+held+"/stop", nil), 202)
	waitTurn(t, lan, held)
	if view := turnOf(held); view.Status != "paused" || !view.Continuable || len(view.Steers) != 1 || view.Steers[0].SteerID != steer.SteerID ||
		steer.Resumed {
		t.Errorf("the held turn %+v %+v", view, steer)
	}
	// A steer to a paused turn continues it.
	if resumed := expect[struct {
		Resumed bool `json:"resumed"`
	}](t, consoleCall(t, lan, "POST", turns+held+"/steer", map[string]string{"text": "Tiếp"}), 202); !resumed.Resumed {
		t.Error("a steer to a paused turn did not continue it")
	}
	waitTurn(t, lan, held)
	if view := turnOf(held); view.Status != "succeeded" {
		t.Errorf("the steered turn %+v", view)
	}

	// A turn waiting on questions takes the answers that fit.
	_, asking := ask(t, lan, "ask")
	waitTurn(t, lan, asking)
	check(t, "answers that do not fit", consoleCall(t, lan, "POST", turns+asking+"/input",
		map[string]any{"answers": map[string]any{"range": "wrong"}}), 422, "agent_answers_invalid")
	expectStatus(t, consoleCall(t, lan, "POST", turns+asking+"/input",
		map[string]any{"answers": map[string]any{"range": "week", "cities": []string{"hn"}}}), 202)
	waitTurn(t, lan, asking)
	agentStub.mu.Lock()
	answered := slices.Contains(agentStub.answers, "The member's answers to your questions:\n- Khoảng: Tuần này\n- Thành phố: Hà Nội")
	agentStub.mu.Unlock()
	if view := turnOf(asking); view.Status != "succeeded" || !answered {
		t.Errorf("the answered turn %+v, answers sent %v", view, answered)
	}

	// A paused turn holds its conversation until it ends.
	paused, pausing := ask(t, lan, "pause")
	waitTurn(t, lan, pausing)
	check(t, "a second question", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+paused+"/turns",
		map[string]string{"text": "?"}), 409, "agent_turn_in_progress")
	check(t, "deleting with a turn open", consoleCall(t, lan, "DELETE", "/v1/console/agent/conversations/"+paused, nil), 409,
		"agent_turn_in_progress")
	if state := expect[struct {
		Status string `json:"status"`
	}](t, consoleCall(t, lan, "POST", turns+pausing+"/end", nil), 200); state.Status != "cancelled" {
		t.Errorf("end %+v", state)
	}

	// A stream that breaks closes the turn as agent_stuck, and Continue
	// runs it again.
	_, broken := ask(t, lan, "break")
	waitTurn(t, lan, broken)
	if view := turnOf(broken); view.Status != "failed" || view.ErrorCode != "agent_stuck" || !view.Continuable {
		t.Errorf("the broken turn %+v", view)
	}
	expectStatus(t, consoleCall(t, lan, "POST", turns+broken+"/continue", nil), 202)
	waitTurn(t, lan, broken)
	if view := turnOf(broken); view.Status != "succeeded" || view.ErrorCode != "" {
		t.Errorf("the continued turn %+v", view)
	}
	check(t, "a question the agent refuses", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+paused+"/turns",
		map[string]string{"text": "down"}), 503, "agent_unavailable")

	// A segment the agent refuses to resume leaves the turn as it was.
	_, stuck := ask(t, lan, "pause-down")
	waitTurn(t, lan, stuck)
	check(t, "continue when the agent refuses", consoleCall(t, lan, "POST", turns+stuck+"/continue", nil), 503, "agent_unavailable")
	check(t, "a steer when the agent refuses", consoleCall(t, lan, "POST", turns+stuck+"/steer", map[string]string{"text": "Tiếp"}), 503,
		"agent_unavailable")
	if view := turnOf(stuck); view.Status != "paused" || len(view.Steers) != 0 {
		t.Errorf("a turn the agent did not resume %+v", view)
	}
	_, waiting := ask(t, lan, "ask-down")
	waitTurn(t, lan, waiting)
	check(t, "answers when the agent refuses", consoleCall(t, lan, "POST", turns+waiting+"/input",
		map[string]any{"answers": map[string]any{"range": "week"}}), 503, "agent_unavailable")
	if view := turnOf(waiting); view.Status != "awaiting_input" {
		t.Errorf("a turn the agent did not resume %+v", view)
	}

	// More than 30 questions and updates a minute are refused.
	busy, _ := ask(t, lan, "pause")
	limited := false
	for range 35 {
		res := consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+busy+"/turns", map[string]string{"text": "?"})
		if res.Status == http.StatusTooManyRequests {
			check(t, "the 31st question", res, 429, "agent_rate_limited")
			limited = true
			break
		}
	}
	if !limited {
		t.Error("no question was rate limited")
	}

	skills(t, lan)

	const conversations = "/v1/console/agent/conversations/"
	renamed := expect[agentConversation](t, consoleCall(t, lan, "PATCH", conversations+conversation.ID,
		map[string]string{"title": "  Tuần này  "}), 200)
	if renamed.ID != conversation.ID || renamed.Title != "Tuần này" {
		t.Errorf("renamed %+v", renamed)
	}
	check(t, "a title of spaces", consoleCall(t, lan, "PATCH", conversations+conversation.ID, map[string]string{"title": "   "}), 422,
		"agent_title_invalid")
	check(t, "a rename without a title", consoleCall(t, lan, "PATCH", conversations+conversation.ID, map[string]any{}), 400, "")
	account, _ := newAccount(t)
	shared := expect[agentConversation](t, consoleCall(t, lan, "POST", "/v1/console/agent/conversations",
		map[string]any{"subject_kind": "account", "subject_id": account.Account.ID}), 200)
	for _, method := range []string{"PATCH", "DELETE"} {
		var body any
		if method == "PATCH" {
			body = map[string]string{"title": "Tuần"}
		}
		check(t, method+" a shared conversation", consoleCall(t, lan, method, conversations+shared.ID, body), 403, "agent_forbidden")
		check(t, method+" an unknown conversation", consoleCall(t, lan, method, conversations+missing, body), 404,
			"agent_conversation_not_found")
		expectStatus(t, consoleCall(t, signedOut, method, conversations+conversation.ID, body), 401)
	}
	expectStatus(t, consoleCall(t, lan, "DELETE", conversations+conversation.ID, nil), 204)
	check(t, "the deleted conversation", consoleCall(t, lan, "GET", conversations+conversation.ID, nil), 404, "agent_conversation_not_found")

	picks := expect[struct {
		Items []struct {
			Name    string `json:"name"`
			Default bool   `json:"default"`
		} `json:"items"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/agents", nil), 200)
	if !slices.ContainsFunc(picks.Items, func(a struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
	},
	) bool {
		return a.Name == "Console assistant" && a.Default
	}) {
		t.Errorf("the agents a member picks %+v", picks)
	}
	expectStatus(t, consoleCall(t, signedOut, "GET", "/v1/console/agents", nil), 401)
}

// ask asks question in a new conversation of member's own and returns
// the conversation and the turn, without waiting for the run.
func ask(t *testing.T, member consoleSession, question string) (conversation, turn string) {
	t.Helper()
	c := expect[agentConversation](t, consoleCall(t, member, "POST", "/v1/console/agent/conversations", map[string]any{}), http.StatusOK)
	started := expect[agentTurnStarted](t, consoleCall(t, member, "POST", "/v1/console/agent/conversations/"+c.ID+"/turns",
		map[string]string{"text": question}), http.StatusAccepted)
	return c.ID, started.TurnID
}

// skills checks the skills: shared by every member, changed only by their
// creator or an admin, each slug once.
func skills(t *testing.T, lan consoleSession) {
	t.Helper()
	const path = "/v1/console/agent/skills"
	other := signInMember(t, consoledomain.RoleModerator)
	slug := "weekly-" + newID()[28:]
	skill := map[string]any{"name": "Weekly", "slug": slug, "prompt": "Report on {{week}}", "variables": []map[string]any{{"name": "week"}}}
	made := expect[struct {
		ID        string `json:"id"`
		CreatedBy string `json:"created_by"`
	}](t, consoleCall(t, lan, "POST", path, skill), 201)
	if made.CreatedBy != lan.ID {
		t.Errorf("made by %s", made.CreatedBy)
	}
	if list := expect[struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}](t, consoleCall(t, other, "GET", path, nil), 200); !slices.ContainsFunc(list.Items, func(s struct {
		Slug string `json:"slug"`
	},
	) bool {
		return s.Slug == slug
	}) {
		t.Errorf("skills %+v", list)
	}
	check(t, "a name of spaces", consoleCall(t, lan, "POST", path, map[string]any{"name": "   ", "prompt": "?"}), 422,
		"agent_skill_invalid")
	check(t, "a skill without a prompt", consoleCall(t, lan, "POST", path, map[string]any{"name": "x", "slug": "x"}), 400, "")
	check(t, "a slug taken", consoleCall(t, other, "POST", path, map[string]any{"name": "x", "slug": slug, "prompt": "?"}), 409,
		"agent_skill_taken")
	expectStatus(t, consoleCall(t, lan, "GET", path+"/"+made.ID, nil), 200)
	check(t, "an unknown skill", consoleCall(t, lan, "GET", path+"/"+newID(), nil), 404, "agent_skill_not_found")
	patch := map[string]any{"name": "Weekly report"}
	check(t, "another member's skill", consoleCall(t, other, "PATCH", path+"/"+made.ID, patch), 403, "agent_forbidden")
	check(t, "deleting another member's skill", consoleCall(t, other, "DELETE", path+"/"+made.ID, nil), 403, "agent_forbidden")
	expectStatus(t, consoleCall(t, lan, "PATCH", path+"/"+made.ID, patch), 200)
	check(t, "an empty patch", consoleCall(t, lan, "PATCH", path+"/"+made.ID, map[string]any{}), 400, "")
	check(t, "a patch to a name of spaces", consoleCall(t, lan, "PATCH", path+"/"+made.ID, map[string]any{"name": "   "}), 422,
		"agent_skill_invalid")
	taken := expect[struct {
		ID string `json:"id"`
	}](t, consoleCall(t, lan, "POST", path, map[string]any{"name": "Taken", "slug": slug + "-2", "prompt": "?"}), 201)
	check(t, "a patch to a slug taken", consoleCall(t, lan, "PATCH", path+"/"+taken.ID, map[string]any{"slug": slug}), 409,
		"agent_skill_taken")
	check(t, "a patch to an unknown skill", consoleCall(t, lan, "PATCH", path+"/"+newID(), patch), 404, "agent_skill_not_found")
	for _, c := range [][2]string{
		{"POST", path},
		{"GET", path},
		{"GET", path + "/" + made.ID},
		{"PATCH", path + "/" + made.ID},
		{"DELETE", path + "/" + made.ID},
	} {
		var body any
		if c[0] == "POST" || c[0] == "PATCH" {
			body = skill
		}
		expectStatus(t, consoleCall(t, consoleSession{}, c[0], c[1], body), 401)
	}
	expectStatus(t, consoleCall(t, lan, "DELETE", path+"/"+made.ID, nil), 204)
	check(t, "deleting an unknown skill", consoleCall(t, lan, "DELETE", path+"/"+made.ID, nil), 404, "agent_skill_not_found")
}
