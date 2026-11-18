package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
)

// A turn's run controls, its tool calls, its documents and the skills go
// to the agent with the member, and the agent's refusals reach the console
// as its codes. UC-MOD-08.
func TestConsoleAgentRuns(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	signedOut := consoleSession{}
	ok, missing, conflict, down := newID(), agentMissing.String(), agentConflict.String(), agentDown.String()
	const turns = "/v1/console/agent/turns/"

	conversation := expect[agentConversation](t, consoleCall(t, lan, "POST", "/v1/console/agent/conversations", map[string]any{}),
		http.StatusOK)
	check(t, "an unknown field", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "?", "speed": "fast"}), 400, "")
	check(t, "an unknown agent", consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "?", "agent_id": newID()}), 404, "ai_agent_not_found")
	started := expect[agentTurnStarted](t, consoleCall(t, lan, "POST", "/v1/console/agent/conversations/"+conversation.ID+"/turns",
		map[string]any{"text": "Tuần này thế nào?", "attachments": []map[string]any{{"kind": "ticket", "id": newID()}}}),
		http.StatusAccepted)
	agentStub.mu.Lock()
	alias := agentStub.configs[len(agentStub.configs)-1]
	agentStub.mu.Unlock()
	if alias != "console-agent" || started.TurnID == "" {
		t.Errorf("the turn went to %q: %+v", alias, started)
	}

	events := consoleCall(t, lan, "GET", turns+started.TurnID+"/events", nil, [2]string{"Last-Event-ID", "1"})
	if events.Status != 200 || strings.Contains(string(events.Body), "turn.started") || !strings.Contains(string(events.Body), "id: 2") ||
		events.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("events after 1: %d %s", events.Status, events.Body)
	}
	if after := consoleCall(t, lan, "GET", turns+started.TurnID+"/events?after=1", nil); after.Status != 200 ||
		strings.Contains(string(after.Body), "turn.started") {
		t.Errorf("events after=1: %d %s", after.Status, after.Body)
	}
	view := expect[struct {
		Status  string `json:"status"`
		LastSeq int    `json:"last_seq"`
		Sources []struct {
			N    int    `json:"n"`
			Tool string `json:"tool"`
		} `json:"sources"`
	}](t, consoleCall(t, lan, "GET", turns+ok, nil), 200)
	if view.Status != "succeeded" || view.LastSeq != 2 || len(view.Sources) != 1 || view.Sources[0].N != 1 {
		t.Errorf("turn %+v", view)
	}
	check(t, "an unknown turn", consoleCall(t, lan, "GET", turns+missing, nil), 404, "agent_turn_not_found")
	check(t, "a turn when the agent fails", consoleCall(t, lan, "GET", turns+down, nil), 503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "GET", turns+ok, nil), 401)
	check(t, "the events of an unknown turn", consoleCall(t, lan, "GET", turns+missing+"/events", nil), 404, "agent_turn_not_found")
	check(t, "the events when the agent fails", consoleCall(t, lan, "GET", turns+down+"/events", nil), 503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "GET", turns+ok+"/events", nil), 401)

	bodies := map[string]any{
		"pause": nil, "stop": nil, "continue": nil, "end": nil,
		"steer": map[string]string{"text": "Chỉ Hà Nội"},
		"input": map[string]any{"answers": map[string]any{"range": "week", "cities": []string{"hn"}}},
	}
	for c, body := range bodies {
		status := http.StatusAccepted
		if c == "end" {
			status = http.StatusOK
		}
		state := expect[struct {
			TurnID string `json:"turn_id"`
			Status string `json:"status"`
		}](t, consoleCall(t, lan, "POST", turns+ok+"/"+c, body), status)
		if state.TurnID != ok || (c == "end") != (state.Status == "cancelled") {
			t.Errorf("%s: %+v", c, state)
		}
		check(t, c+" an unknown turn", consoleCall(t, lan, "POST", turns+missing+"/"+c, body), 404, "agent_turn_not_found")
		check(t, c+" a turn in another state", consoleCall(t, lan, "POST", turns+conflict+"/"+c, body), 409, "agent_turn_state")
		check(t, c+" when the agent fails", consoleCall(t, lan, "POST", turns+down+"/"+c, body), 503, "agent_unavailable")
		expectStatus(t, consoleCall(t, signedOut, "POST", turns+ok+"/"+c, body), 401)
	}
	check(t, "a steer without text", consoleCall(t, lan, "POST", turns+ok+"/steer", map[string]any{}), 400, "")
	check(t, "input without answers", consoleCall(t, lan, "POST", turns+ok+"/input", map[string]any{"answers": map[string]any{}}), 400, "")
	check(t, "answers that do not fit", consoleCall(t, lan, "POST", turns+ok+"/input", map[string]any{"answers": map[string]any{"range": "wrong"}}),
		422, "agent_answers_invalid")

	for path, code := range map[string]string{
		"/v1/console/agent/tool-calls/": "agent_tool_call_not_found", "/v1/console/agent/documents/": "agent_document_not_found",
	} {
		expectStatus(t, consoleCall(t, lan, "GET", path+ok, nil), 200)
		check(t, path+" unknown", consoleCall(t, lan, "GET", path+missing, nil), 404, code)
		check(t, path+" when the agent fails", consoleCall(t, lan, "GET", path+down, nil), 503, "agent_unavailable")
		expectStatus(t, consoleCall(t, signedOut, "GET", path+ok, nil), 401)
	}
	if doc := expect[struct {
		LatestVersion int   `json:"latest_version"`
		Versions      []any `json:"versions"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/agent/documents/"+ok, nil), 200); doc.LatestVersion != 2 ||
		len(doc.Versions) != 2 {
		t.Errorf("document %+v", doc)
	}

	const skills = "/v1/console/agent/skills"
	if list := expect[struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}](t, consoleCall(t, lan, "GET", skills, nil), 200); len(list.Items) != 1 {
		t.Errorf("skills %+v", list)
	}
	expectStatus(t, consoleCall(t, signedOut, "GET", skills, nil), 401)
	skill := map[string]any{"name": "Weekly", "slug": "weekly", "prompt": "Report on {{week}}", "variables": []map[string]any{{"name": "week"}}}
	if made := expect[struct {
		CreatedBy string `json:"created_by"`
	}](t, consoleCall(t, lan, "POST", skills, skill), 201); made.CreatedBy != lan.ID {
		t.Errorf("made by %s", made.CreatedBy)
	}
	check(t, "a skill the agent refuses", consoleCall(t, lan, "POST", skills, map[string]any{"name": "invalid", "prompt": "?"}), 422,
		"agent_skill_invalid")
	expectStatus(t, consoleCall(t, lan, "GET", skills+"/"+ok, nil), 200)
	check(t, "an unknown skill", consoleCall(t, lan, "GET", skills+"/"+missing, nil), 404, "agent_skill_not_found")
	check(t, "a skill when the agent fails", consoleCall(t, lan, "GET", skills+"/"+down, nil), 503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "GET", skills+"/"+ok, nil), 401)
	forbidden := agentForbidden.String()
	check(t, "another member's skill", consoleCall(t, lan, "PATCH", skills+"/"+forbidden, map[string]any{"name": "x"}), 403, "agent_forbidden")
	check(t, "deleting another member's skill", consoleCall(t, lan, "DELETE", skills+"/"+forbidden, nil), 403, "agent_forbidden")
	check(t, "a patch the agent refuses", consoleCall(t, lan, "PATCH", skills+"/"+ok, map[string]any{"name": "invalid"}), 422,
		"agent_skill_invalid")
	check(t, "a skill without a prompt", consoleCall(t, lan, "POST", skills, map[string]any{"name": "x", "slug": "x"}), 400, "")
	check(t, "a slug taken", consoleCall(t, lan, "POST", skills, map[string]any{"name": "x", "slug": "taken", "prompt": "?"}), 409,
		"agent_skill_taken")
	check(t, "a skill when the agent fails", consoleCall(t, lan, "POST", skills, map[string]any{"name": "x", "slug": "down", "prompt": "?"}),
		503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "POST", skills, skill), 401)
	patch := map[string]any{"name": "Weekly report"}
	expectStatus(t, consoleCall(t, lan, "PATCH", skills+"/"+ok, patch), 200)
	check(t, "an empty patch", consoleCall(t, lan, "PATCH", skills+"/"+ok, map[string]any{}), 400, "")
	check(t, "an unknown skill", consoleCall(t, lan, "PATCH", skills+"/"+missing, patch), 404, "agent_skill_not_found")
	check(t, "a patch to a slug taken", consoleCall(t, lan, "PATCH", skills+"/"+conflict, patch), 409, "agent_skill_taken")
	check(t, "a patch when the agent fails", consoleCall(t, lan, "PATCH", skills+"/"+down, patch), 503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "PATCH", skills+"/"+ok, patch), 401)
	expectStatus(t, consoleCall(t, lan, "DELETE", skills+"/"+ok, nil), 204)
	check(t, "deleting an unknown skill", consoleCall(t, lan, "DELETE", skills+"/"+missing, nil), 404, "agent_skill_not_found")
	check(t, "deleting when the agent fails", consoleCall(t, lan, "DELETE", skills+"/"+down, nil), 503, "agent_unavailable")
	expectStatus(t, consoleCall(t, signedOut, "DELETE", skills+"/"+ok, nil), 401)

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
