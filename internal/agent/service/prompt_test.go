package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// The model reads these exactly; a change here changes what every turn
// reads, so it is a change to make on purpose.
func TestPromptText(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 27, 9, 30, 0, 0, time.UTC)
	lan := member("Lan", "moderator", "m1")
	if got := factsText(at, lan, true, "account a1", ""); got != "Time: 2026-11-27 09:30 UTC\nStaff member: Lan (moderator, id m1)\n"+
		"This conversation is about account a1." {
		t.Errorf("first facts %q", got)
	}
	if got := factsText(at, lan, false, "", ""); got != "Time: 2026-11-27 09:30 UTC" {
		t.Errorf("later facts %q", got)
	}
	if got := steerText(""); got != "Written while you worked." {
		t.Errorf("own steer %q", got)
	}
	if got := steerText(lan); got != "Written while you worked, by Lan (moderator, id m1)." {
		t.Errorf("another's steer %q", got)
	}
	ok, conflict := 200, 409
	got := decisionsText([]repository.Approval{
		{Operation: "suspendAccount", Path: "/v1/console/accounts/a1/suspend", Status: domain.ApprovalApproved, ResultStatus: &ok},
		{Operation: "suspendAccount", Path: "/v1/console/accounts/a2/suspend", Status: domain.ApprovalRejected},
		{
			Operation: "suspendAccount", Path: "/v1/console/accounts/a3/suspend", Status: domain.ApprovalFailed, ResultStatus: &conflict,
			ResultBody: json.RawMessage(`{"code":"account_suspended"}`),
		},
	})
	want := "Changes you asked for, decided since:\n" +
		"- suspendAccount /v1/console/accounts/a1/suspend: approved\n" +
		"- suspendAccount /v1/console/accounts/a2/suspend: rejected\n" +
		"- suspendAccount /v1/console/accounts/a3/suspend: failed, the call answered 409 {\"code\":\"account_suspended\"}\n"
	if got != want {
		t.Errorf("decisions\n%s\nwant\n%s", got, want)
	}
	if s := instructionsOf(repository.Agent{Instructions: "Help.\n"}); !strings.HasPrefix(s, "Help.\n\n<console>\n") ||
		!strings.HasSuffix(s, "</console>") {
		t.Errorf("instructions %q", s)
	}
}
