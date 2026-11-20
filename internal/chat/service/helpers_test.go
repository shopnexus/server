package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	catalog  *catalogsvc.Catalog
	media    *mediasvc.Media
	deps     service.Deps
	chat     *service.Chat
}

// newEnv builds chat and the modules it calls on a database and the test
// bucket, and serves media's and chat's flows under one prefix.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, media, in, p)
	deps := service.Deps{
		Chats: repository.NewChats(db), QuickReplies: repository.NewQuickReplies(db), Accounts: accounts, Catalog: c, Media: media,
		Ingress: in, Prefix: p, Now: time.Now,
	}
	chat := service.New(deps)
	restatetest.Start(t, append(media.Durable(), chat.Durable()...)...)
	return env{db: db, accounts: accounts, catalog: c, media: media, deps: deps, chat: chat}
}

// person creates an account with a verified phone number.
func (e env) person(t *testing.T) uuid.UUID {
	t.Helper()
	return accounttest.WithPhone(t, e.db)
}

// upload creates a JPEG upload for owner and PUTs body to it.
func (e env) upload(t *testing.T, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "image/jpeg", body)
}

func key() string { return uuid.NewV7().String() }

// open has buyer open a conversation from listing with text.
func (e env) open(t *testing.T, buyer, listing uuid.UUID, text string) service.ConversationView {
	t.Helper()
	v, err := e.chat.Open(context.Background(), buyer, key(), service.OpenInput{ListingID: listing, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// messages lists what viewer sees of a conversation, newest first.
func (e env) messages(t *testing.T, viewer, conversation uuid.UUID) []service.MessageView {
	t.Helper()
	p, err := e.chat.Messages(context.Background(), viewer, conversation, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	return p.Items
}

// settled waits until message id leaves processing for its sender.
func (e env) settled(t *testing.T, sender, conversation, id uuid.UUID) service.MessageView {
	t.Helper()
	var m service.MessageView
	testenv.Eventually(t, 30*time.Second, func() error {
		ms := e.messages(t, sender, conversation)
		for i := range ms {
			if ms[i].ID != id {
				continue
			}
			if ms[i].Status == domain.StatusProcessing {
				return fmt.Errorf("message %s still processing", id)
			}
			m = ms[i]
			return nil
		}
		return fmt.Errorf("message %s not in the conversation", id)
	})
	return m
}
