package service_test

import (
	"context"
	"net/netip"
	"testing"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

type addEnv struct {
	add         ingress.Requester[service.AddIdentityRun, service.AddIdentityResult]
	credentials repository.Credentials
	challenges  repository.Challenges
}

func startAddIdentity(t *testing.T, changes ...func(*service.AddIdentity)) *addEnv {
	t.Helper()
	db := pgtest.New(t)
	svc := service.AddIdentity{
		Prefix: restatetest.Prefix(), Credentials: repository.NewCredentials(db), Challenges: repository.NewChallenges(db),
		Now: restatetest.NewClock(now).Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &addEnv{
		add:         ingress.Service[service.AddIdentityRun, service.AddIdentityResult](client, svc.ServiceName(), "Run"),
		credentials: repository.NewCredentials(db), challenges: repository.NewChallenges(db),
	}
}

// account signs up with addr through the store.
func (e *addEnv) account(t *testing.T, addr contact.Address) uuid.UUID {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.credentials.SignUp(context.Background(), a, domain.NewCredentials(a.ID(), domain.NewIdentity(uuid.NewV7(), addr, now), now)); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

// addChallenge stores an add_identity challenge of owner's for to and
// returns the guess its code makes.
func (e *addEnv) addChallenge(t *testing.T, owner uuid.UUID, to string) *service.CodeGuess {
	t.Helper()
	id := uuid.NewV7()
	dest := email(t, to)
	hash := testCodes.Hash(dest, testCodes.For(id))
	c := domain.NewChallenge(id, domain.PurposeAddIdentity, dest, &owner, netip.MustParseAddr("127.0.0.1"), hash, now)
	if err := e.challenges.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return &service.CodeGuess{ChallengeID: id, CodeHash: hash}
}

func (e *addEnv) run(t *testing.T, in service.AddIdentityRun) service.AddIdentityResult {
	t.Helper()
	res, err := e.add.Request(context.Background(), in, restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAddIdentityAddsAPhone(t *testing.T) {
	t.Parallel()
	e := startAddIdentity(t)
	acc := e.account(t, email(t, "linh@example.com"))
	res := e.run(t, service.AddIdentityRun{AccountID: acc, Address: new(phone(t, "0901234567"))})
	if res.Refused != nil || len(res.Identities) != 2 {
		t.Fatalf("%+v", res)
	}
	ref, err := e.credentials.FindIdentity(context.Background(), domain.KindPhone, "+84901234567")
	if err != nil || ref.AccountID != acc {
		t.Fatalf("%+v %v", ref, err)
	}
}

func TestAddIdentityByEmailCode(t *testing.T) {
	t.Parallel()
	e := startAddIdentity(t)
	acc := e.account(t, phone(t, "0901234567"))
	res := e.run(t, service.AddIdentityRun{AccountID: acc, Guess: e.addChallenge(t, acc, "linh@example.com")})
	if res.Refused != nil || len(res.Identities) != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestAddIdentityRefusesAnotherAccountsChallenge(t *testing.T) {
	t.Parallel()
	e := startAddIdentity(t)
	owner := e.account(t, phone(t, "0901234567"))
	thief := e.account(t, phone(t, "0912345678"))
	res := e.run(t, service.AddIdentityRun{AccountID: thief, Guess: e.addChallenge(t, owner, "linh@example.com")})
	if res.Refused == nil || res.Refused.Code != "code_expired" {
		t.Fatalf("%+v", res)
	}
}

func TestAddIdentityRefusals(t *testing.T) {
	t.Parallel()
	e := startAddIdentity(t)
	acc := e.account(t, phone(t, "0901234567"))
	e.account(t, email(t, "taken@example.com"))
	if res := e.run(t, service.AddIdentityRun{AccountID: acc, Address: new(phone(t, "0912345678"))}); res.Refused == nil ||
		res.Refused.Code != "identity_kind_taken" {
		t.Fatalf("second phone: %+v", res)
	}
	if res := e.run(t, service.AddIdentityRun{AccountID: acc, Address: new(email(t, "taken@example.com"))}); res.Refused == nil ||
		res.Refused.Code != "identity_taken" {
		t.Fatalf("taken email: %+v", res)
	}
}

// staleCredentials makes the first Save lose a race, as a second
// AddIdentity of the same account would.
type staleCredentials struct {
	repository.Credentials

	lost bool
}

func (s *staleCredentials) Save(ctx context.Context, c *domain.Credentials, writeID uuid.UUID) error {
	if !s.lost {
		s.lost = true
		return repository.ErrConflict
	}
	return s.Credentials.Save(ctx, c, writeID)
}

func TestAddIdentityLostRaceIsTryAgain(t *testing.T) {
	t.Parallel()
	e := startAddIdentity(t, func(s *service.AddIdentity) { s.Credentials = &staleCredentials{Credentials: s.Credentials} })
	acc := e.account(t, email(t, "linh@example.com"))
	if res := e.run(t, service.AddIdentityRun{AccountID: acc, Address: new(phone(t, "0901234567"))}); res.Refused == nil ||
		res.Refused.Code != "try_again" {
		t.Fatalf("%+v", res)
	}
}
