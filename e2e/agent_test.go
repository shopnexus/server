package e2e

import (
	"net/http"
	"slices"
	"testing"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

type agentConversation struct {
	ID          string `json:"id"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Title       string `json:"title"`
}

type agentAnswer struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
	Answer         string `json:"answer"`
	Sources        []struct {
		Tool string   `json:"tool"`
		IDs  []string `json:"ids"`
	} `json:"sources"`
}

// A record's conversation is shared by every member; one without a record
// is the member's own. Each question goes out with the member who asked.
// UC-MOD-08 A1, E1.
func TestConsoleAgentConversations(t *testing.T) {
	t.Parallel()
	lan := signInMember(t, consoledomain.RoleModerator)
	minh := signInMember(t, consoledomain.RoleAdmin)
	account, _ := newAccount(t)
	const path = "/v1/console/agent/conversations"

	subject := map[string]any{"subject_kind": "account", "subject_id": account.Account.ID}
	first := expect[agentConversation](t, consoleCall(t, lan, "POST", path, subject), http.StatusOK)
	if first.SubjectKind != "account" || first.SubjectID != account.Account.ID {
		t.Fatalf("the record's conversation: %+v", first)
	}
	if again := expect[agentConversation](t, consoleCall(t, minh, "POST", path, subject), http.StatusOK); again.ID != first.ID {
		t.Fatalf("another member opens another conversation: %s", again.ID)
	}
	found := expect[struct {
		Items []agentConversation `json:"items"`
	}](t, consoleCall(t, minh, "GET", path+"?subject_kind=account&subject_id="+account.Account.ID, nil), http.StatusOK)
	if len(found.Items) != 1 || found.Items[0].ID != first.ID {
		t.Fatalf("the record's conversation by subject: %+v", found)
	}
	own := expect[agentConversation](t, consoleCall(t, lan, "POST", path, map[string]any{}), http.StatusOK)
	mine := expect[struct {
		Items []agentConversation `json:"items"`
	}](t, consoleCall(t, lan, "GET", path, nil), http.StatusOK)
	if len(mine.Items) != 1 || mine.Items[0].ID != own.ID || own.SubjectKind != "" {
		t.Fatalf("the member's own: %+v", mine)
	}
	ofKind := expect[struct {
		Items []agentConversation `json:"items"`
	}](t, consoleCall(t, minh, "GET", path+"?subject_kind=account", nil), http.StatusOK)
	if !slices.ContainsFunc(ofKind.Items, func(c agentConversation) bool { return c.ID == first.ID }) {
		t.Fatalf("the conversations about accounts: %+v", ofKind)
	}

	answer := expect[agentAnswer](t, consoleCall(t, lan, "POST", path+"/"+first.ID+"/turns", map[string]string{"text": "Tài khoản này có gì lạ?"}),
		http.StatusOK)
	if answer.ConversationID != first.ID || answer.TurnID == "" || answer.Answer == "" || len(answer.Sources) != 1 ||
		answer.Sources[0].Tool != "ticket_get" {
		t.Fatalf("answer %+v", answer)
	}
	if asked := agentStub.lastAsked(); asked != [2]string{lan.ID, "moderator"} {
		t.Fatalf("the agent heard %v, want %s as moderator", asked, lan.ID)
	}
	detail := expect[struct {
		agentConversation

		Turns []struct {
			MemberID string `json:"member_id"`
			Status   string `json:"status"`
		} `json:"turns"`
	}](t, consoleCall(t, minh, "GET", path+"/"+first.ID, nil), http.StatusOK)
	if detail.ID != first.ID || len(detail.Turns) != 1 || detail.Turns[0].MemberID != lan.ID ||
		detail.Turns[0].Status != "succeeded" {
		t.Fatalf("detail %+v", detail)
	}

	turns := path + "/" + first.ID + "/turns"
	for text, want := range map[string]struct {
		status int
		code   string
	}{
		"budget": {429, "agent_budget"},
		"rate":   {429, "agent_rate_limited"},
		"slow":   {504, "agent_timeout"},
		"busy":   {409, "agent_turn_in_progress"},
		"closed": {409, "agent_conversation_closed"},
		"long":   {422, "agent_input_rejected"},
		"down":   {503, "agent_unavailable"},
	} {
		check(t, text, consoleCall(t, lan, "POST", turns, map[string]string{"text": text}), want.status, want.code)
	}
	check(t, "an unknown conversation", consoleCall(t, lan, "POST", path+"/"+newID()+"/turns", map[string]string{"text": "?"}),
		404, "agent_conversation_not_found")
	check(t, "an unknown conversation", consoleCall(t, lan, "GET", path+"/"+newID(), nil), 404, "agent_conversation_not_found")
	check(t, "an id without a kind", consoleCall(t, lan, "GET", path+"?subject_id="+account.Account.ID, nil), 422, "agent_subject_invalid")
	check(t, "a kind without an id", consoleCall(t, lan, "POST", path, map[string]any{"subject_kind": "ticket"}), 422,
		"agent_subject_invalid")

	down := agentDown.String()
	check(t, "the agent fails", consoleCall(t, lan, "GET", path+"?subject_kind=ticket&subject_id="+down, nil), 503, "agent_unavailable")
	check(t, "the agent fails", consoleCall(t, lan, "POST", path, map[string]any{"subject_kind": "ticket", "subject_id": down}), 503,
		"agent_unavailable")
	check(t, "the agent fails", consoleCall(t, lan, "GET", path+"/"+down, nil), 503, "agent_unavailable")

	signedOut := consoleSession{}
	expectStatus(t, consoleCall(t, signedOut, "GET", path, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", path, map[string]any{}), 401)
	expectStatus(t, consoleCall(t, signedOut, "GET", path+"/"+first.ID, nil), 401)
	expectStatus(t, consoleCall(t, signedOut, "POST", turns, map[string]string{"text": "?"}), 401)
}
