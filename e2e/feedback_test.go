package e2e

import (
	"net/http"
	"testing"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
)

// feedbackBody is the Feedback schema, as far as the tests read it.
type feedbackBody struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	Status         string `json:"status"`
	StatusNote     string `json:"status_note"`
	ShippedVersion string `json:"shipped_version"`
	Others         int    `json:"others"`
	Reply          *struct {
		Text string `json:"text"`
	} `json:"reply"`
}

// Two people's feedback becomes one group; shipping it tells both, and
// the reply tells its owner.
func TestFeedbackFromSendToShipped(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	phone := "phone-" + me.Account.ID
	putToken(t, me.AccessToken, phone)
	draft := map[string]any{
		"kind": "idea", "text": "Thêm bộ lọc size khung", "contact": true, "screen": "search_results",
		"device": map[string]any{"model": "Pixel 9", "os": "Android 16", "app_version": "2.5.0"},
	}
	key := newKey()
	mine := expect[feedbackBody](t, call(t, "POST", "/v1/feedback", draft, bearer(me.AccessToken), key), http.StatusCreated)
	if again := expect[feedbackBody](t, call(t, "POST", "/v1/feedback", draft, bearer(me.AccessToken), key), http.StatusOK); again.ID != mine.ID {
		t.Fatalf("a repeated key: %s", again.ID)
	}
	theirs := expect[feedbackBody](t, call(t, "POST", "/v1/feedback", map[string]any{"mood": 4, "contact": false},
		bearer(other.AccessToken), newKey()), http.StatusCreated)
	check(t, "neither mood nor text", call(t, "POST", "/v1/feedback", map[string]any{"text": "  ", "contact": false},
		bearer(me.AccessToken), newKey()), 422, "invalid_feedback")
	check(t, "an unknown field", call(t, "POST", "/v1/feedback", map[string]any{"mood": 1, "contact": false, "rating": 5},
		bearer(me.AccessToken), newKey()), 400, "")

	mod := signInMember(t, consoledomain.RoleModerator)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/feedback/"+theirs.ID+"/same-as", map[string]any{"feedback_id": mine.ID}),
		http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/feedback/"+theirs.ID+"/status",
		map[string]any{"status": "shipped", "note": "Bộ lọc Size khung đã có", "version": "2.5"}), http.StatusOK)
	if m := phones.waitTitled(t, phone, "Góp ý"); m.Body != "Điều bạn góp ý đã có trong bản 2.5" || m.Data["target_kind"] != "feedback" ||
		m.Data["target_id"] != mine.ID {
		t.Fatalf("push %+v", m)
	}
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/feedback/"+mine.ID+"/reply", map[string]any{"text": "Cảm ơn bạn"}), http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "PUT", "/v1/console/feedback/"+mine.ID+"/heart", nil), http.StatusOK)
	list := expect[struct {
		Items []feedbackBody `json:"items"`
	}](t, call(t, "GET", "/v1/feedback", nil, bearer(me.AccessToken)), http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].Status != "shipped" || list.Items[0].ShippedVersion != "2.5" || list.Items[0].Others != 1 ||
		list.Items[0].StatusNote != "Bộ lọc Size khung đã có" || list.Items[0].Reply == nil || list.Items[0].Reply.Text != "Cảm ơn bạn" {
		t.Fatalf("mine %+v", list.Items)
	}
	phones.wait(t, phone, 2)
	queue := expect[struct {
		Items []struct {
			Feedback  feedbackBody `json:"feedback"`
			AccountID string       `json:"account_id"`
			RootID    string       `json:"root_id"`
		} `json:"items"`
	}](t, consoleCall(t, mod, "GET", "/v1/console/feedback?status=shipped&limit=100", nil), http.StatusOK)
	var found bool
	for _, it := range queue.Items {
		found = found || (it.Feedback.ID == theirs.ID && it.AccountID == other.Account.ID && it.RootID == mine.ID)
	}
	if !found {
		t.Fatalf("queue %+v", queue.Items)
	}
	type person struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	detail := expect[struct {
		Item struct {
			Feedback feedbackBody `json:"feedback"`
			Person   person       `json:"person"`
		} `json:"item"`
		Group []struct {
			Feedback feedbackBody `json:"feedback"`
		} `json:"group"`
	}](t, consoleCall(t, mod, "GET", "/v1/console/feedback/"+theirs.ID, nil), http.StatusOK)
	if detail.Item.Person.ID != other.Account.ID || detail.Item.Person.Status != "active" || len(detail.Group) != 1 || detail.Group[0].Feedback.ID != mine.ID {
		t.Fatalf("detail %+v", detail)
	}
	groups := expect[struct {
		Items []struct {
			Root struct {
				Feedback feedbackBody `json:"feedback"`
			} `json:"root"`
			Size int `json:"size"`
		} `json:"items"`
	}](t, consoleCall(t, mod, "GET", "/v1/console/feedback/groups?kind=idea", nil), http.StatusOK)
	found = false
	for _, g := range groups.Items {
		found = found || (g.Root.Feedback.ID == mine.ID && g.Size == 2)
	}
	if !found {
		t.Fatalf("groups %+v", groups.Items)
	}
	if sum := expect[struct {
		Sent    int   `json:"sent"`
		DaySent []int `json:"day_sent"`
	}](t, consoleCall(t, mod, "GET", "/v1/console/feedback/summary", nil), http.StatusOK); sum.Sent < 2 || len(sum.DaySent) != 30 {
		t.Fatalf("summary %+v", sum)
	}
	for _, p := range []string{"/v1/console/feedback", "/v1/console/feedback/groups", "/v1/console/feedback/summary", "/v1/console/feedback/" + mine.ID} {
		expectStatus(t, consoleCall(t, consoleSession{}, "GET", p, nil), http.StatusUnauthorized)
	}
}

// Every refusal the feedback operations declare.
func TestFeedbackRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	check(t, "send signed out", call(t, "POST", "/v1/feedback", map[string]any{"mood": 3, "contact": false}, newKey()), 401, "unauthenticated")
	check(t, "list signed out", call(t, "GET", "/v1/feedback", nil), 401, "unauthenticated")
	for range 20 {
		expectStatus(t, call(t, "POST", "/v1/feedback", map[string]any{"mood": 3, "contact": false}, bearer(me.AccessToken), newKey()),
			http.StatusCreated)
	}
	check(t, "the 21st", call(t, "POST", "/v1/feedback", map[string]any{"mood": 3, "contact": false}, bearer(me.AccessToken), newKey()),
		429, "too_many_feedback")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	f := expect[feedbackBody](t, call(t, "POST", "/v1/feedback", map[string]any{"text": "Vì sao tôi bị tạm khóa?", "contact": true},
		bearer(suspended.AccessToken), newKey()), http.StatusCreated)

	mod := signInMember(t, consoledomain.RoleModerator)
	none := "/v1/console/feedback/" + newID()
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/status", map[string]any{"status": "seen"}},
		{"POST", "/reply", map[string]any{"text": "Chào bạn"}},
		{"PUT", "/heart", nil},
		{"DELETE", "/heart", nil},
		{"POST", "/same-as", map[string]any{"feedback_id": f.ID}},
	} {
		check(t, c.method+" "+c.path+" of none", consoleCall(t, mod, c.method, none+c.path, c.body), 404, "feedback_not_found")
		expectStatus(t, consoleCall(t, consoleSession{}, c.method, "/v1/console/feedback/"+f.ID+c.path, c.body), http.StatusUnauthorized)
	}
	check(t, "GET of none", consoleCall(t, mod, "GET", none, nil), 404, "feedback_not_found")
	one := "/v1/console/feedback/" + f.ID
	check(t, "a version without shipped", consoleCall(t, mod, "POST", one+"/status", map[string]any{"status": "in_progress", "version": "2.6"}),
		422, "invalid_feedback")
	check(t, "a blank reply", consoleCall(t, mod, "POST", one+"/reply", map[string]any{"text": "   "}), 422, "invalid_feedback")
	check(t, "same as itself", consoleCall(t, mod, "POST", one+"/same-as", map[string]any{"feedback_id": f.ID}), 422, "invalid_feedback")
	expectStatus(t, consoleCall(t, mod, "PUT", one+"/heart", nil), http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "DELETE", one+"/heart", nil), http.StatusOK)
}
