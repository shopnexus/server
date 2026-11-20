package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/provider/push"
)

func TestFCMSend(t *testing.T) {
	t.Parallel()
	type tag struct {
		Tag string `json:"tag"`
	}
	var got struct {
		Message struct {
			Token        string            `json:"token"`
			Notification map[string]string `json:"notification"`
			Data         map[string]string `json:"data"`
			Android      struct {
				CollapseKey  string `json:"collapse_key"`
				Notification tag    `json:"notification"`
			} `json:"android"`
			Webpush struct {
				Headers      map[string]string `json:"headers"`
				Notification tag               `json:"notification"`
			} `json:"webpush"`
		} `json:"message"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/p/messages:send" || json.NewDecoder(r.Body).Decode(&got) != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
	}))
	defer srv.Close()
	f := push.NewFCMWith(srv.Client(), srv.URL, "p")
	m := push.Message{Title: "Trả giá", Body: "Đức Huy trả 1,2 tr", Tag: "deal:1", Data: map[string]string{"type": "offer"}}
	if err := f.Send(context.Background(), "tok", m); err != nil {
		t.Fatal(err)
	}
	g := got.Message
	if g.Token != "tok" || g.Notification["title"] != "Trả giá" || g.Notification["body"] != "Đức Huy trả 1,2 tr" || g.Data["type"] != "offer" {
		t.Fatalf("message: %+v", g)
	}
	if g.Android.CollapseKey != "deal:1" || g.Android.Notification.Tag != "deal:1" {
		t.Fatalf("android: %+v", g.Android)
	}
	if g.Webpush.Notification.Tag != "deal:1" || g.Webpush.Headers["Urgency"] != "high" {
		t.Fatalf("webpush: %+v", g.Webpush)
	}
}

func TestFCMErrors(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"unregistered":     {404, `{"error":{"code":404,"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, push.ErrDeadToken},
		"invalid argument": {400, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, push.ErrDeadToken},
		"throttled":        {429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, push.ErrRetry},
		"unavailable":      {503, `{}`, push.ErrRetry},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		err := push.NewFCMWith(srv.Client(), srv.URL, "p").Send(context.Background(), "tok", push.Message{Title: "x"})
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	err := push.NewFCMWith(forbidden.Client(), forbidden.URL, "p").Send(context.Background(), "tok", push.Message{Title: "x"})
	if err == nil || errors.Is(err, push.ErrDeadToken) || errors.Is(err, push.ErrRetry) {
		t.Fatalf("403: %v", err)
	}
}
