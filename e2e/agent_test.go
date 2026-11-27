package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
)

const agentPath = "/v1/console/agent"

type agentConversation struct {
	ID          string `json:"id"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Title       string `json:"title"`
}

type agentTurnStarted struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
}

type agentTurn struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	MemberID       string `json:"member_id"`
	Agent          struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Stop   string `json:"stop"`
	Error  string `json:"error"`
	Usage  struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"usage"`
}

type agentApproval struct {
	ID           string          `json:"id"`
	TurnID       string          `json:"turn_id"`
	Operation    string          `json:"operation"`
	Method       string          `json:"method"`
	Path         string          `json:"path"`
	Body         json.RawMessage `json:"body"`
	Status       string          `json:"status"`
	DecidedBy    string          `json:"decided_by"`
	ResultStatus *int            `json:"result_status"`
}

type agentTurnState struct {
	TurnID string `json:"turn_id"`
	Status string `json:"status"`
}

// newConversation opens a conversation: about the record subject names
// (kind, id), or of s's own when there is none.
func newConversation(t *testing.T, s consoleSession, subject ...string) agentConversation {
	t.Helper()
	body := map[string]any{}
	if len(subject) == 2 {
		body = map[string]any{"subject_kind": subject[0], "subject_id": subject[1]}
	}
	return expect[agentConversation](t, consoleCall(t, s, "POST", agentPath+"/conversations", body), http.StatusOK)
}

// ask starts a turn in a conversation, with an agent when one is given.
func ask(t *testing.T, s consoleSession, conversation, text string, agentID ...string) agentTurnStarted {
	t.Helper()
	body := map[string]any{"text": text}
	if len(agentID) > 0 {
		body["agent_id"] = agentID[0]
	}
	return expect[agentTurnStarted](t, consoleCall(t, s, "POST", agentPath+"/conversations/"+conversation+"/turns", body),
		http.StatusAccepted)
}

// waitTurn waits for a turn's run to end, and reads the turn.
func waitTurn(t *testing.T, s consoleSession, id string) agentTurn {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		turn := expect[agentTurn](t, consoleCall(t, s, "GET", agentPath+"/turns/"+id, nil), http.StatusOK)
		if turn.Status != "running" {
			return turn
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn %s still runs", id)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// askAndWait asks a question in a new conversation of s's own and waits
// for the answer.
func askAndWait(t *testing.T, s consoleSession, text string) agentTurn {
	t.Helper()
	return waitTurn(t, s, ask(t, s, newConversation(t, s).ID, text).TurnID)
}

// hold starts a turn that runs until it is paused or stopped, and gives
// it with its token.
func hold(t *testing.T, s consoleSession, conversation string, agentID ...string) (started agentTurnStarted, token string) {
	t.Helper()
	started = ask(t, s, conversation, "hold", agentID...)
	if !agentStub.holding(started.TurnID) {
		t.Fatalf("turn %s never ran", started.TurnID)
	}
	return started, agentStub.ran(started.TurnID).Token
}

// control posts a control of a turn.
func control(t *testing.T, s consoleSession, id, what string, body any) Response {
	t.Helper()
	return consoleCall(t, s, "POST", agentPath+"/turns/"+id+"/"+what, body)
}

// turnCall calls the server as an agent's turn: with its token, as its
// CLI does. The answer is not checked against the spec, which describes a
// member's calls.
func turnCall(t *testing.T, token, method, path string, body any) (status int, answer []byte) {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, server+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Requested-With", "shopnexus-console")
	req.Header.Set("X-Forwarded-For", testAddress(t))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	answer, _ = io.ReadAll(resp.Body)
	return resp.StatusCode, answer
}

// A record's conversation is shared by every member; one without a record
// is the member's own. A question runs on the agent runtime over the
// conversation's history, as the member who asked.
// UC-MOD-08 A5, A6, E1, E2, E4, E6, E7.
func TestConsoleAgentConversations(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	minh := signInMember(t, consoledomain.RoleAdmin)
	account, _ := newAccount(t)
	const path = agentPath + "/conversations"

	first := newConversation(t, lan, "account", account.Account.ID)
	if first.SubjectKind != "account" || first.SubjectID != account.Account.ID {
		t.Fatalf("the record's conversation: %+v", first)
	}
	if again := newConversation(t, minh, "account", account.Account.ID); again.ID != first.ID {
		t.Fatalf("another member opens another conversation: %s", again.ID)
	}
	found := expect[page[agentConversation]](t, consoleCall(t, minh, "GET", path+"?subject_kind=account&subject_id="+account.Account.ID, nil),
		http.StatusOK)
	if len(found.Items) != 1 || found.Items[0].ID != first.ID {
		t.Fatalf("the record's conversation by subject: %+v", found)
	}
	own := newConversation(t, lan)
	mine := expect[page[agentConversation]](t, consoleCall(t, lan, "GET", path, nil), http.StatusOK)
	if len(mine.Items) != 1 || mine.Items[0].ID != own.ID || own.SubjectKind != "" {
		t.Fatalf("the member's own: %+v", mine)
	}
	ofKind := expect[page[agentConversation]](t, consoleCall(t, minh, "GET", path+"?subject_kind=account", nil), http.StatusOK)
	if !slices.ContainsFunc(ofKind.Items, func(c agentConversation) bool { return c.ID == first.ID }) {
		t.Fatalf("the conversations about accounts: %+v", ofKind)
	}

	started := ask(t, lan, first.ID, "Tài khoản này có gì lạ?")
	if started.ConversationID != first.ID || started.TurnID == "" {
		t.Fatalf("started %+v", started)
	}
	turn := waitTurn(t, lan, started.TurnID)
	if turn.Status != "succeeded" || turn.Stop != "done" || turn.MemberID != lan.ID || turn.Agent.Name != "Console assistant" ||
		turn.Model == "" || turn.Usage.InputTokens != 100 || turn.Usage.CachedTokens != 40 {
		t.Fatalf("turn %+v", turn)
	}
	sent := agentStub.ran(started.TurnID)
	if len(sent.History) != 1 || !strings.Contains(sent.Instructions, "<console>") || sent.Model != turn.Model || sent.ModelKey == "" ||
		!strings.HasPrefix(sent.Token, "snt_") {
		t.Fatalf("the runtime got %+v", sent)
	}
	if facts := sent.History[0].Context; !strings.Contains(facts, "("+string(consoledomain.RoleModerator)+", id "+lan.ID+")") ||
		!strings.Contains(facts, "about account "+account.Account.ID) {
		t.Fatalf("the question's context %q", facts)
	}
	events := consoleCall(t, minh, "GET", agentPath+"/turns/"+started.TurnID+"/events", nil)
	expectStatus(t, events, http.StatusOK)
	for _, typ := range []string{"turn.started", "text.delta", "step", "turn.finished"} {
		if !strings.Contains(string(events.Body), `"type":"`+typ+`"`) {
			t.Errorf("no %s in %s", typ, events.Body)
		}
	}
	check(t, "events after a bad seq", consoleCall(t, minh, "GET", agentPath+"/turns/"+started.TurnID+"/events?after=x", nil), 400, "")
	check(t, "the events of an unknown turn", consoleCall(t, minh, "GET", agentPath+"/turns/"+newID()+"/events", nil), 404,
		"agent_turn_not_found")

	// The next question goes out over the history: the first question and
	// its answer.
	second := ask(t, minh, first.ID, "Còn gì nữa?")
	waitTurn(t, minh, second.TurnID)
	if h := agentStub.ran(second.TurnID).History; len(h) != 3 || h[0].Content != "Tài khoản này có gì lạ?" ||
		h[1].Content != "Đã xong." || h[2].Content != "Còn gì nữa?" {
		t.Fatalf("the second turn's history %+v", h)
	}
	// The record is told once; a member is told when they first write.
	if h := agentStub.ran(second.TurnID).History; !strings.Contains(h[2].Context, "id "+minh.ID+")") ||
		strings.Contains(h[2].Context, "about account") {
		t.Fatalf("the second question's context %q", h[2].Context)
	}
	third := ask(t, minh, first.ID, "Thêm nữa?")
	waitTurn(t, minh, third.TurnID)
	if h := agentStub.ran(third.TurnID).History; strings.Contains(h[4].Context, "Staff member") || !strings.HasPrefix(h[4].Context, "Time: ") {
		t.Fatalf("the third question's context %q", h[4].Context)
	}
	detail := expect[struct {
		agentConversation

		Messages []struct {
			Seq     int    `json:"seq"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Turns []agentTurn `json:"turns"`
	}](t, consoleCall(t, minh, "GET", path+"/"+first.ID, nil), http.StatusOK)
	if detail.ID != first.ID || len(detail.Messages) != 6 || detail.Messages[3].Content != "Đã xong." || len(detail.Turns) != 3 ||
		detail.Turns[0].MemberID != lan.ID || detail.Turns[1].MemberID != minh.ID {
		t.Fatalf("detail %+v", detail)
	}

	turns := path + "/" + first.ID + "/turns"
	if spent := askAndWait(t, lan, "budget"); spent.Status != "failed" || spent.Stop != "budget" {
		t.Fatalf("a turn past the agent's budget %+v", spent)
	}
	check(t, "the runtime down", consoleCall(t, lan, "POST", turns, map[string]string{"text": "down"}), 503, "agent_unavailable")
	if again := expect[struct {
		Turns []agentTurn `json:"turns"`
	}](t, consoleCall(t, lan, "GET", path+"/"+first.ID, nil), http.StatusOK); len(again.Turns) != 3 {
		t.Fatalf("a turn the runtime refused is kept: %+v", again.Turns)
	}
	check(t, "no text", consoleCall(t, lan, "POST", turns, map[string]string{}), 400, "")
	check(t, "an unknown agent", consoleCall(t, lan, "POST", turns, map[string]string{"text": "?", "agent_id": newID()}), 404,
		"ai_agent_not_found")
	key := [2]string{"Idempotency-Key", newID()}
	keyed := expect[agentTurnStarted](t, consoleCall(t, lan, "POST", turns, map[string]string{"text": "Một lần thôi"}, key), 202)
	waitTurn(t, lan, keyed.TurnID)
	if again := expect[agentTurnStarted](t, consoleCall(t, lan, "POST", turns, map[string]string{"text": "Một lần thôi"}, key),
		202); again.TurnID != keyed.TurnID {
		t.Errorf("the same key started turn %s, want %s", again.TurnID, keyed.TurnID)
	}
	check(t, "only a zero-width space", consoleCall(t, lan, "POST", turns, map[string]string{"text": "\u200b"}), 422, "agent_input_rejected")
	check(t, "an unknown conversation", consoleCall(t, lan, "POST", path+"/"+newID()+"/turns", map[string]string{"text": "?"}),
		404, "agent_conversation_not_found")
	check(t, "another member's own", consoleCall(t, minh, "POST", path+"/"+own.ID+"/turns", map[string]string{"text": "?"}),
		404, "agent_conversation_not_found")
	check(t, "another member's own turn", consoleCall(t, minh, "GET", agentPath+"/turns/"+askAndWait(t, lan, "?").ID, nil), 404,
		"agent_turn_not_found")
	check(t, "an unknown conversation", consoleCall(t, lan, "GET", path+"/"+newID(), nil), 404, "agent_conversation_not_found")
	check(t, "an id without a kind", consoleCall(t, lan, "GET", path+"?subject_id="+account.Account.ID, nil), 422, "agent_subject_invalid")
	check(t, "an unknown kind", consoleCall(t, lan, "GET", path+"?subject_kind=planet", nil), 400, "")
	check(t, "a kind without an id", consoleCall(t, lan, "POST", path, map[string]any{"subject_kind": "ticket"}), 422,
		"agent_subject_invalid")

	// A member renames and deletes their own; a shared one is an admin's.
	renamed := expect[agentConversation](t, consoleCall(t, lan, "PATCH", path+"/"+own.ID, map[string]string{"title": "Của tôi"}), 200)
	if renamed.Title != "Của tôi" {
		t.Fatalf("renamed %+v", renamed)
	}
	check(t, "a blank title", consoleCall(t, lan, "PATCH", path+"/"+own.ID, map[string]string{"title": "   "}), 422, "agent_title_invalid")
	check(t, "no title", consoleCall(t, lan, "PATCH", path+"/"+own.ID, map[string]string{}), 400, "")
	check(t, "a shared one", consoleCall(t, lan, "PATCH", path+"/"+first.ID, map[string]string{"title": "x"}), 403, "agent_forbidden")
	check(t, "another member's own", consoleCall(t, minh, "PATCH", path+"/"+newConversation(t, lan).ID, map[string]string{"title": "x"}),
		200, "")
	check(t, "an unknown one", consoleCall(t, lan, "PATCH", path+"/"+newID(), map[string]string{"title": "x"}), 404,
		"agent_conversation_not_found")
	check(t, "a shared one", consoleCall(t, lan, "DELETE", path+"/"+first.ID, nil), 403, "agent_forbidden")
	expectStatus(t, consoleCall(t, lan, "DELETE", path+"/"+own.ID, nil), 204)
	check(t, "a deleted one", consoleCall(t, lan, "DELETE", path+"/"+own.ID, nil), 404, "agent_conversation_not_found")

	agents := expect[page[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}]](t, consoleCall(t, lan, "GET", "/v1/console/agents", nil), 200)
	if len(agents.Items) == 0 {
		t.Fatal("no console agent")
	}

	signedOut := consoleSession{}
	expectStatus(t, consoleCall(t, signedOut, "GET", path, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", path, map[string]any{}), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", path+"/"+first.ID, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "PATCH", path+"/"+first.ID, map[string]string{"title": "x"}), 401)
	expectStatus(t, consoleCall(t, signedOut, "DELETE", path+"/"+first.ID, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", turns, map[string]string{"text": "?"}), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", agentPath+"/turns/"+started.TurnID, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", agentPath+"/turns/"+started.TurnID+"/events", nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", "/v1/console/agents", nil), 401)
}

// A turn's token calls the console API as the member, within its agent's
// capabilities; a change waits for a member's approval, which runs it as
// the approver, and the agent hears of the decision in the next run.
// The AI settings count what it did.
// UC-MOD-08 A2, A3, A4 · UC-MOD-12 A4.
func TestConsoleAgentApprovals(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	minh := signInMember(t, consoledomain.RoleAdmin)
	suffix := strings.ToLower(newID()[:8])
	mod := expect[aiAgent](t, consoleCall(t, minh, "POST", settings+"/agents", map[string]any{
		"name": "Moderator " + suffix, "slug": "moderator-" + suffix, "model": haiku, "instructions": "Moderate.",
		"capabilities": []string{"accounts.read", "accounts.suspend"},
	}), 201)
	spammer, _ := newAccount(t)
	other, _ := newAccount(t)
	conversation := newConversation(t, lan, "account", spammer.Account.ID)
	started, token := hold(t, lan, conversation.ID, mod.ID)
	accounts := "/v1/console/accounts/"

	if status, body := turnCall(t, token, "GET", accounts+spammer.Account.ID, nil); status != 200 {
		t.Fatalf("a read the agent may make: %d %s", status, body)
	}
	for name, path := range map[string]string{
		"a read the agent may not make": "/v1/console/tickets",
		"an app route":                  "/v1/me",
		"the console's members":         "/v1/console/members",
	} {
		if status, body := turnCall(t, token, "GET", path, nil); status != 403 || !strings.Contains(string(body), "agent_operation_forbidden") {
			t.Errorf("%s: %d %s", name, status, body)
		}
	}
	if status, _ := turnCall(t, "snt_"+strings.Repeat("A", 43), "GET", accounts+spammer.Account.ID, nil); status != 401 {
		t.Errorf("an unknown token: %d", status)
	}
	var ops struct {
		Operations []struct {
			Name     string `json:"name"`
			Approval bool   `json:"approval"`
		} `json:"operations"`
	}
	status, body := turnCall(t, token, "GET", agentPath+"/operations", nil)
	_ = json.Unmarshal(body, &ops)
	names := map[string]bool{}
	for _, op := range ops.Operations {
		names[op.Name] = op.Approval
	}
	if approval, ok := names["getAccount"]; status != 200 || len(names) != 5 || !names["suspendAccount"] || !ok || approval {
		t.Fatalf("the agent's operations: %d %s", status, body)
	}
	if all := expect[struct {
		Operations []struct {
			Name string `json:"name"`
		} `json:"operations"`
	}](t, consoleCall(t, lan, "GET", agentPath+"/operations", nil), 200); len(all.Operations) <= len(names) {
		t.Errorf("every operation an agent may be given: %d", len(all.Operations))
	}

	// A change is held: it is checked against the spec, and does not run.
	if refused, answer := turnCall(t, token, "POST", accounts+spammer.Account.ID+"/suspend", map[string]any{}); refused != 400 {
		t.Errorf("a change the spec refuses: %d %s", refused, answer)
	}
	// The model reads only the approval's id and status: the rest is what
	// it sent.
	type heldChange struct {
		ApprovalID string `json:"approval_id"`
		Status     string `json:"status"`
	}
	var held heldChange
	status, body = turnCall(t, token, "POST", accounts+spammer.Account.ID+"/suspend", map[string]string{"reason": "spam"})
	_ = json.Unmarshal(body, &held)
	if status != 202 || held.Status != "pending" || held.ApprovalID == "" || len(body) > 80 {
		t.Fatalf("a held change: %d %s", status, body)
	}
	status, body = turnCall(t, token, "POST", accounts+other.Account.ID+"/suspend", map[string]string{"reason": "spam"})
	var heldOther heldChange
	_ = json.Unmarshal(body, &heldOther)
	if status != 202 {
		t.Fatalf("a second held change: %d %s", status, body)
	}
	read := func(id string) string {
		return expect[struct {
			Account struct {
				Status string `json:"status"`
			} `json:"account"`
		}](t, consoleCall(t, minh, "GET", accounts+id, nil), 200).Account.Status
	}
	if s := read(spammer.Account.ID); s != "active" {
		t.Fatalf("a held change ran: %s", s)
	}
	var seen page[agentApproval]
	status, body = turnCall(t, token, "GET", agentPath+"/approvals?status=pending", nil)
	_ = json.Unmarshal(body, &seen)
	if status != 200 || len(seen.Items) != 2 || seen.Items[0].Operation != "suspendAccount" || seen.Items[0].TurnID != started.TurnID {
		t.Fatalf("the turn's approvals: %d %s", status, body)
	}
	listed := expect[page[agentApproval]](t, consoleCall(t, minh, "GET", agentPath+"/approvals?conversation_id="+conversation.ID, nil), 200)
	if len(listed.Items) != 2 {
		t.Fatalf("the conversation's approvals: %+v", listed)
	}
	check(t, "an unknown status", consoleCall(t, minh, "GET", agentPath+"/approvals?status=maybe", nil), 400, "")

	// A member steers the turn, then pauses it; its token ends with the run.
	expect[agentTurnState](t, control(t, lan, started.TurnID, "steer", map[string]string{"text": "Chỉ khoá tài khoản spam."}), 202)
	check(t, "a steer without text", control(t, lan, started.TurnID, "steer", map[string]string{}), 400, "")
	expect[agentTurnState](t, control(t, lan, started.TurnID, "pause", nil), 202)
	if paused := waitTurn(t, lan, started.TurnID); paused.Status != "paused" || paused.Stop != "paused" {
		t.Fatalf("paused %+v", paused)
	}
	if events := consoleCall(t, lan, "GET", agentPath+"/turns/"+started.TurnID+"/events", nil); !strings.Contains(string(events.Body),
		`"type":"steer"`) || !strings.Contains(string(events.Body), "Chỉ khoá tài khoản spam.") {
		t.Errorf("no steer in the turn's events: %s", events.Body)
	}
	if steers := agentStub.ran(started.TurnID).Steers; len(steers) != 1 || steers[0].Content != "Chỉ khoá tài khoản spam." ||
		steers[0].Context != "Written while you worked." {
		t.Fatalf("steers %+v", steers)
	}
	if status, _ := turnCall(t, token, "GET", accounts+spammer.Account.ID, nil); status != 401 {
		t.Errorf("the token of a run that ended: %d", status)
	}

	// The approver runs the change as themselves; a decided one stays so.
	approved := expect[agentApproval](t, consoleCall(t, minh, "POST", agentPath+"/approvals/"+held.ApprovalID+"/approve", nil), 200)
	if approved.Status != "approved" || approved.ResultStatus == nil || *approved.ResultStatus != 200 || approved.DecidedBy != minh.ID {
		t.Fatalf("approved %+v", approved)
	}
	if s := read(spammer.Account.ID); s != "suspended" {
		t.Fatalf("the approved change did not run: %s", s)
	}
	check(t, "approved twice", consoleCall(t, lan, "POST", agentPath+"/approvals/"+held.ApprovalID+"/approve", nil), 409,
		"agent_approval_decided")
	check(t, "rejected once approved", consoleCall(t, lan, "POST", agentPath+"/approvals/"+held.ApprovalID+"/reject", nil), 409,
		"agent_approval_decided")
	rejected := expect[agentApproval](t, consoleCall(t, lan, "POST", agentPath+"/approvals/"+heldOther.ApprovalID+"/reject", nil), 200)
	if rejected.Status != "rejected" || rejected.ResultStatus != nil || read(other.Account.ID) != "active" {
		t.Fatalf("rejected %+v", rejected)
	}
	check(t, "approve an unknown one", consoleCall(t, lan, "POST", agentPath+"/approvals/"+newID()+"/approve", nil), 404,
		"agent_approval_not_found")
	check(t, "reject an unknown one", consoleCall(t, lan, "POST", agentPath+"/approvals/"+newID()+"/reject", nil), 404,
		"agent_approval_not_found")

	// The resumed run hears of both decisions, once.
	expect[agentTurnState](t, control(t, lan, started.TurnID, "resume", nil), 202)
	if done := waitTurn(t, lan, started.TurnID); done.Status != "succeeded" {
		t.Fatalf("resumed %+v", done)
	}
	h := agentStub.ran(started.TurnID).History
	if len(h) < 2 || h[len(h)-1].Role != "user" || h[len(h)-1].Content != "" ||
		!strings.Contains(h[len(h)-1].Context, "suspendAccount "+accounts+spammer.Account.ID+"/suspend: approved\n") ||
		!strings.Contains(h[len(h)-1].Context, "suspendAccount "+accounts+other.Account.ID+"/suspend: rejected\n") {
		t.Fatalf("the resumed run's history %+v", h)
	}
	next := ask(t, lan, conversation.ID, "Xong chưa?")
	waitTurn(t, lan, next.TurnID)
	if h := agentStub.ran(next.TurnID).History; strings.Contains(h[len(h)-1].Context, "decided since") {
		t.Errorf("a decision told twice: %q", h[len(h)-1].Context)
	}

	// The AI settings count what the agent did: its calls by operation,
	// its approvals by status and its turns by how they ended.
	a := expect[agentActivity](t, consoleCall(t, minh, "GET", settings+"/agents/activity?range=30d", nil), 200)
	calls := map[string][3]int{}
	for _, c := range a.Calls {
		if c.AgentID == mod.ID {
			calls[c.Operation] = [3]int{c.Calls, c.Held, c.Errors}
		}
	}
	approvals := map[string]int{}
	for _, x := range a.Approvals {
		if x.AgentID == mod.ID {
			approvals[x.Status] = x.Approvals
		}
	}
	turnsDone := 0
	for _, x := range a.Turns {
		if x.AgentID == mod.ID && x.Status == "succeeded" && x.Stop == "done" {
			turnsDone = x.Turns
		}
	}
	if a.Range != "30d" || calls["suspendAccount"] != [3]int{3, 2, 1} || calls["getAccount"][0] < 1 ||
		approvals["approved"] != 1 || approvals["rejected"] != 1 || turnsDone != 1 {
		t.Fatalf("activity %+v", a)
	}

	signedOut := consoleSession{}
	expectStatus(t, consoleCall(t, signedOut, "GET", agentPath+"/operations", nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", agentPath+"/approvals", nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", agentPath+"/approvals/"+held.ApprovalID+"/approve", nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", agentPath+"/approvals/"+held.ApprovalID+"/reject", nil), 401)
}

// Pause waits for the commands running, stop cuts the run, resume runs the
// conversation's last turn again; each takes a turn in the state it needs.
// UC-MOD-08 A1, A2, E3.
func TestConsoleAgentControls(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	minh := signInMember(t, consoledomain.RoleAdmin)
	conversation := newConversation(t, lan)
	started, _ := hold(t, lan, conversation.ID)
	turns := agentPath + "/conversations/" + conversation.ID + "/turns"

	check(t, "a second turn", consoleCall(t, lan, "POST", turns, map[string]string{"text": "?"}), 409, "agent_turn_in_progress")
	check(t, "resume a running turn", control(t, lan, started.TurnID, "resume", nil), 409, "agent_turn_state")
	check(t, "delete while it runs", consoleCall(t, lan, "DELETE", agentPath+"/conversations/"+conversation.ID, nil), 409,
		"agent_turn_in_progress")

	agentStub.refuse(conversation.ID, true)
	for _, what := range []string{"pause", "stop", "steer"} {
		check(t, what+" with the runtime down", control(t, lan, started.TurnID, what, map[string]string{"text": "?"}), 503,
			"agent_unavailable")
	}
	agentStub.refuse(conversation.ID, false)
	expect[agentTurnState](t, control(t, lan, started.TurnID, "stop", nil), 202)
	stopped := waitTurn(t, lan, started.TurnID)
	if stopped.Status != "stopped" || stopped.Stop != "stopped" {
		t.Fatalf("stopped %+v", stopped)
	}
	events := consoleCall(t, lan, "GET", agentPath+"/turns/"+started.TurnID+"/events", nil)
	if !strings.Contains(string(events.Body), `"status":"stopped"`) {
		t.Fatalf("events %s", events.Body)
	}
	for _, what := range []string{"pause", "stop", "steer"} {
		check(t, what+" a turn that ended", control(t, lan, started.TurnID, what, map[string]string{"text": "?"}), 409, "agent_turn_state")
		check(t, what+" an unknown turn", control(t, lan, newID(), what, map[string]string{"text": "?"}), 404, "agent_turn_not_found")
	}
	check(t, "resume an unknown turn", control(t, lan, newID(), "resume", nil), 404, "agent_turn_not_found")
	check(t, "resume another member's own", control(t, minh, started.TurnID, "resume", nil), 404, "agent_turn_not_found")

	agentStub.refuse(conversation.ID, true)
	check(t, "resume with the runtime down", control(t, lan, started.TurnID, "resume", nil), 503, "agent_unavailable")
	agentStub.refuse(conversation.ID, false)
	if still := waitTurn(t, lan, started.TurnID); still.Status != "stopped" {
		t.Fatalf("a resume that did not start changed the turn: %+v", still)
	}
	expect[agentTurnState](t, control(t, lan, started.TurnID, "resume", nil), 202)
	if done := waitTurn(t, lan, started.TurnID); done.Status != "succeeded" {
		t.Fatalf("resumed %+v", done)
	}
	waitTurn(t, lan, ask(t, lan, conversation.ID, "?").TurnID)
	check(t, "resume a turn not the last", control(t, lan, started.TurnID, "resume", nil), 409, "agent_turn_state")

	signedOut := consoleSession{}
	for _, what := range []string{"pause", "stop", "resume", "steer"} {
		expectStatus(t, control(t, signedOut, started.TurnID, what, map[string]string{"text": "?"}), 401)
	}
}

// A member asks at most 30 times a minute; the AI settings count the
// refusals.
// UC-MOD-08 E5 · UC-MOD-12 A4.
func TestConsoleAgentRateLimit(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	turns := agentPath + "/conversations/" + newConversation(t, lan).ID + "/turns"
	for range 30 {
		expectStatus(t, consoleCall(t, lan, "POST", turns, map[string]string{"text": "\u200b"}), 422)
	}
	check(t, "the 31st", consoleCall(t, lan, "POST", turns, map[string]string{"text": "?"}), 429, "agent_rate_limited")
	minh := signInMember(t, consoledomain.RoleAdmin)
	if a := expect[agentActivity](t, consoleCall(t, minh, "GET", settings+"/agents/activity", nil), 200); a.RateLimited < 1 || a.Range != "7d" {
		t.Fatalf("activity %+v", a)
	}
	check(t, "an unknown range", consoleCall(t, minh, "GET", settings+"/agents/activity?range=1y", nil), 400, "")
}

// agentActivity is what the AI settings read of the agents' activity.
type agentActivity struct {
	Range string `json:"range"`
	Calls []struct {
		AgentID   string `json:"agent_id"`
		Operation string `json:"operation"`
		Calls     int    `json:"calls"`
		Held      int    `json:"held"`
		Errors    int    `json:"errors"`
	} `json:"calls"`
	Turns []struct {
		AgentID string `json:"agent_id"`
		Status  string `json:"status"`
		Stop    string `json:"stop"`
		Turns   int    `json:"turns"`
	} `json:"turns"`
	Approvals []struct {
		AgentID   string `json:"agent_id"`
		Status    string `json:"status"`
		Approvals int    `json:"approvals"`
	} `json:"approvals"`
	RateLimited int `json:"rate_limited"`
}
