package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/testenv"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

// embedEnv is newEnv with counting embedding fakes.
func embedEnv(t *testing.T) (e env, text, image *embedtest.Model) {
	t.Helper()
	text, image = embedtest.New(domain.TextDims), embedtest.New(domain.PhotoDims)
	e = newEnv(t, func(d *service.Deps) { d.Text, d.Image = text, image })
	return e, text, image
}

// vectors waits until listing has a text vector and want photo vectors.
func vectors(t *testing.T, e env, id uuid.UUID, want int) repository.VectorState {
	t.Helper()
	vs := repository.NewVectors(e.db)
	var st repository.VectorState
	testenv.Eventually(t, 20*time.Second, func() error {
		var err error
		if st, err = vs.State(context.Background(), id, "text-test", "image-test"); err != nil {
			return err
		}
		if st.TextHash == nil || len(st.Photos) != want {
			return fmt.Errorf("text %v, %d photos", st.TextHash != nil, len(st.Photos))
		}
		return nil
	})
	return st
}

func TestPublishEmbeds(t *testing.T) {
	t.Parallel()
	e, text, image := embedEnv(t)
	v := e.post(t, e.seller(t)) // post waits until the listing is active, as the other flow tests do
	vectors(t, e, v.ID, 1)
	if text.Calls() != 1 || image.Calls() != 1 {
		t.Fatalf("text %d, image %d", text.Calls(), image.Calls())
	}
}

func TestTextEditEmbedsNoPhoto(t *testing.T) {
	t.Parallel()
	e, text, image := embedEnv(t)
	seller := e.seller(t)
	v := e.post(t, seller)
	vectors(t, e, v.ID, 1)
	d := draft(v.Photos[0].UploadID) // the same photo; name the field as ListingView has it
	d.Description = "Máy zin, mới thay pin"
	if _, err := e.catalog.Edit(context.Background(), seller, v.ID, uuid.NewV7().String(), d); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 20*time.Second, func() error {
		if text.Calls() != 2 {
			return fmt.Errorf("text %d", text.Calls())
		}
		return nil
	})
	if image.Calls() != 1 {
		t.Fatalf("a text edit embedded a photo again: %d", image.Calls())
	}
}

func TestBurstOfEditsEmbedsEachPhotoOnce(t *testing.T) {
	t.Parallel()
	e, _, image := embedEnv(t)
	seller := e.seller(t)
	v := e.post(t, seller)
	for i := range 5 {
		d := draft(v.Photos[0].UploadID)
		d.Description = fmt.Sprintf("lần %d", i)
		if _, err := e.catalog.Edit(context.Background(), seller, v.ID, uuid.NewV7().String(), d); err != nil {
			t.Fatal(err)
		}
	}
	vectors(t, e, v.ID, 1)
	time.Sleep(2 * time.Second) // let the queued runs drain; each finds nothing to do
	if image.Calls() != 1 {
		t.Fatalf("image %d", image.Calls())
	}
}

func TestReplacedPhotoLosesItsVector(t *testing.T) {
	t.Parallel()
	e, _, _ := embedEnv(t)
	seller := e.seller(t)
	v := e.post(t, seller)
	before := vectors(t, e, v.ID, 1)
	fresh := e.upload(t, seller, mediatest.JPEG(t, 80, 60))
	if _, err := e.catalog.Edit(context.Background(), seller, v.ID, uuid.NewV7().String(), draft(fresh)); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 20*time.Second, func() error {
		st, err := repository.NewVectors(e.db).State(context.Background(), v.ID, "text-test", "image-test")
		if err != nil {
			return err
		}
		if len(st.Photos) != 1 || st.Photos[0] == before.Photos[0] {
			return fmt.Errorf("%v", st.Photos)
		}
		return nil
	})
}

func TestEmbedRetriesUntilTheModelAnswers(t *testing.T) {
	t.Parallel()
	e, _, image := embedEnv(t)
	image.SetFail(errors.New("503"))
	v := e.post(t, e.seller(t))
	testenv.Eventually(t, 20*time.Second, func() error {
		if image.Calls() < 2 {
			return fmt.Errorf("%d calls", image.Calls())
		}
		return nil
	})
	image.SetFail(nil)
	vectors(t, e, v.ID, 1)
}

func TestRefreshReembedsAnotherVersion(t *testing.T) {
	t.Parallel()
	e, _, image := embedEnv(t)
	v := e.post(t, e.seller(t))
	st := vectors(t, e, v.ID, 1)
	if _, err := e.db.Exec(context.Background(), `UPDATE listing_photo_vectors SET model_version = 'old' WHERE listing_id = @id`,
		pgx.StrictNamedArgs{"id": v.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.ObjectSend[restate.Void](e.ingress, e.prefix+service.ListingEmbedName, v.ID.String(), "Refresh").
		Send(context.Background(), restate.Void{}); err != nil {
		t.Fatal(err)
	}
	vectors(t, e, v.ID, len(st.Photos))
	if image.Calls() != 2 {
		t.Fatalf("image %d", image.Calls())
	}
}
