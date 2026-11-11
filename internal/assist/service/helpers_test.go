package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/assist/repository"
	"github.com/shopnexus/server2/internal/assist/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/mediatest"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
	"github.com/shopnexus/server2/internal/provider/chotot"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
	"github.com/shopnexus/server2/internal/provider/frames"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// transcript is what the fake STT hears in a seller's video.
const transcript = "mình bán xe đạp asama giá một triệu hai có thương lượng"

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	media    *mediasvc.Media
	assist   *service.Assist
	videos   repository.VideoReads
	clock    *restatetest.Clock
	ingress  *ingress.Client
	prefix   string
	cut      *fakeCutter
	stt      *fakeSTT
	model    *fakeModel
	links    repository.LinkReads
	source   *fakeSource
	deps     service.Deps
}

// newEnv serves modules media, catalog and assist on the test stack, with
// fakes for ai's frames, STT and the model, and a hand-moved clock. Each
// option changes the fakes or the dependencies before assist is built.
func newEnv(t *testing.T, opts ...func(*env)) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	m := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, m, in, p)
	e := env{
		db: db, accounts: accounts, media: m, videos: repository.NewVideoReads(db),
		clock: restatetest.NewClock(time.Now()), ingress: in, prefix: p,
		cut:    &fakeCutter{frames: 3, jpeg: mediatest.JPEG(t, 96, 64)},
		stt:    &fakeSTT{text: transcript},
		model:  &fakeModel{answer: bicycle(), link: iphoneAnswer()},
		links:  repository.NewLinkReads(db),
		source: newSource(t),
	}
	e.deps = service.Deps{
		VideoReads: e.videos, LinkReads: e.links, Source: e.source, Accounts: accounts, Media: m, Catalog: c,
		Frames: e.cut, STT: e.stt, Model: e.model,
		Text: embedtest.New(catalogdomain.TextDims), Image: embedtest.New(catalogdomain.PhotoDims),
		TextMin: 0.5, PhotoMin: 0.5, CallTimeout: 5 * time.Second,
		Log: slog.New(slog.DiscardHandler), Ingress: in, Prefix: p, Now: e.clock.Now,
	}
	for _, o := range opts {
		o(&e)
	}
	e.assist = service.New(e.deps)
	restatetest.Start(t, slices.Concat(m.Durable(), c.Durable(), e.assist.Durable())...)
	return e
}

// video uploads a short mp4 for owner and returns its upload ID.
func (e env) video(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "video/mp4", []byte("a phone video of a bicycle"))
}

// start starts a read of a fresh video of owner's.
func (e env) start(t *testing.T, owner uuid.UUID) service.VideoReadView {
	t.Helper()
	v, err := e.assist.StartVideoRead(context.Background(), owner, uuid.NewV7().String(), e.video(t, owner))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// end waits until the read leaves reading and returns it.
func (e env) end(t *testing.T, owner, id uuid.UUID) service.VideoReadView {
	t.Helper()
	var v service.VideoReadView
	testenv.Eventually(t, 30*time.Second, func() error {
		var err error
		if v, err = e.assist.GetVideoRead(context.Background(), owner, id); err != nil {
			t.Fatal(err)
		}
		if v.Status == domain.StatusReading {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return v
}

// updates counts the video_read updates of the read in owner's log.
func (e env) updates(t *testing.T, owner, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM account_updates
WHERE account_id = @account AND kind = 'video_read' AND video_read_id = @read`,
		pgx.StrictNamedArgs{"account": owner, "read": id}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// videoOf is the upload the read started from.
func (e env) videoOf(t *testing.T, id uuid.UUID) mediadomain.Status {
	t.Helper()
	r, err := e.videos.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := mediarepo.NewUploads(e.db).Get(context.Background(), r.VideoID())
	if err != nil {
		t.Fatal(err)
	}
	return u.Status()
}

// fakeCutter answers as ai's frames endpoint: frames copies of jpeg with
// rising sharpness, or err. onCall runs before each answer.
type fakeCutter struct {
	mu     sync.Mutex
	frames int
	jpeg   []byte
	err    error
	onCall func()
}

func (f *fakeCutter) Extract(_ context.Context, video io.Reader, _ int64) (frames.Result, error) {
	if _, err := io.Copy(io.Discard, video); err != nil {
		return frames.Result{}, err
	}
	f.mu.Lock()
	n, jpeg, err, onCall := f.frames, f.jpeg, f.err, f.onCall
	f.mu.Unlock()
	if onCall != nil {
		onCall()
	}
	if err != nil {
		return frames.Result{}, err
	}
	res := frames.Result{Duration: 12}
	for i := range n {
		res.Frames = append(res.Frames, frames.Frame{AtSeconds: float64(i) * 1.5, Sharpness: float64(100 + i), JPEG: jpeg})
	}
	return res, nil
}

// fakeSTT hears text, or fails with err.
type fakeSTT struct {
	mu   sync.Mutex
	text string
	err  error
}

func (f *fakeSTT) Transcribe(_ context.Context, audio io.Reader, _ int64) (string, error) {
	if _, err := io.Copy(io.Discard, audio); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.err
}

// fakeModel answers every request with answer, or fails with err, and
// keeps what it was asked.
type fakeModel struct {
	mu     sync.Mutex
	answer domain.Answer
	err    error
	link   domain.LinkAnswer
	calls  int
	images int
	user   string
	system string
}

func (m *fakeModel) Complete(_ context.Context, r llm.Request, out any) error {
	m.mu.Lock()
	m.calls++
	m.images, m.user, m.system = len(r.Images), r.User, r.System
	var answer any = m.answer
	if r.SchemaName == domain.LinkAnswerSchemaName {
		answer = m.link
	}
	err := m.err
	m.mu.Unlock()
	if err != nil {
		return err
	}
	b, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (m *fakeModel) asked() (calls, images int, user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.images, m.user
}

// bicycle is the model's answer for the seller's bicycle: frames 2 and 0
// kept, the price spoken, a range of its own.
func bicycle() domain.Answer {
	price := int64(1_200_000)
	return domain.Answer{
		IsItem: true, Title: "Xe đạp Asama xanh", CategoryCode: "vehicles.bicycle", Condition: "used",
		Specs:       []catalogdomain.Spec{{Label: "Màu", Value: "Xanh"}},
		Description: "Xe đi ít, giá một triệu hai, có thương lượng.",
		SpokenPrice: &price, Negotiable: true, Kept: []int{2, 0},
		Estimate: &domain.Range{Low: 1_000_000, Median: 1_200_000, High: 1_400_000},
	}
}

// photoURL is photo i of the fake listing on the fake CDN.
func photoURL(i int) string { return fmt.Sprintf("https://cdn.chotot.com/fake/%d.jpg", i) }

// fakeSource is Chợ Tốt with one listing, the seller's iPhone, and its
// five photos. adErr or imageErr fails every call of its kind.
type fakeSource struct {
	mu       sync.Mutex
	ad       chotot.Ad
	adErr    error
	images   map[string][]byte
	imageErr error
	adCalls  int
	// onAd runs before each answer of Ad, outside the lock.
	onAd func()
}

func newSource(t *testing.T) *fakeSource {
	t.Helper()
	f := &fakeSource{images: map[string][]byte{}, ad: chotot.Ad{
		Subject:      "iPhone 11 64GB trắng pin 81% zin all",
		Body:         "Máy dùng 3 năm, zin all, chưa sửa chữa. Pin 81%. Có thương lượng. Liên hệ 0909 123 456.",
		Price:        4_500_000,
		CategoryName: "Điện thoại",
		Params:       []chotot.Param{{Label: "Dung lượng", Value: "64 GB"}, {Label: "Màu sắc", Value: "Trắng"}},
	}}
	for i := range 5 {
		f.ad.Images = append(f.ad.Images, photoURL(i))
		f.images[photoURL(i)] = mediatest.JPEG(t, 64+i, 48)
	}
	return f
}

func (f *fakeSource) Ad(_ context.Context, listID int64) (chotot.Ad, error) {
	f.mu.Lock()
	onAd := f.onAd
	f.mu.Unlock()
	if onAd != nil {
		onAd()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adCalls++
	if f.adErr != nil {
		return chotot.Ad{}, f.adErr
	}
	a := f.ad
	a.ListID = listID
	return a, nil
}

func (f *fakeSource) Image(_ context.Context, url string) (photo []byte, contentType string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.imageErr != nil {
		return nil, "", f.imageErr
	}
	b, ok := f.images[url]
	if !ok {
		return nil, "", chotot.ErrBadImage
	}
	return b, "image/jpeg", nil
}

// iphoneAnswer is the model's answer for the seller's iPhone; it leaves
// the phone in the description for the regex to find.
func iphoneAnswer() domain.LinkAnswer {
	return domain.LinkAnswer{
		IsItem: true, Title: "iPhone 11 64GB Trắng, pin 81%", CategoryCode: "phones.iphone", Condition: "used",
		Specs:       []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}, {Label: "Màu", Value: "Trắng"}},
		Description: "Máy dùng 3 năm, zin all, chưa sửa chữa. Pin 81%. Có thương lượng. Liên hệ 0909 123 456.",
		Negotiable:  true, Estimate: &domain.Range{Low: 4_000_000, Median: 4_500_000, High: 5_000_000},
	}
}

const iphoneLink = "https://www.chotot.com/tp-ho-chi-minh/quan-binh-thanh/mua-ban-dien-thoai/119482736.htm?utm_source=share"

// startLink starts a read of the fake listing as owner.
func (e env) startLink(t *testing.T, owner uuid.UUID) service.LinkReadView {
	t.Helper()
	v, err := e.assist.StartLinkRead(context.Background(), owner, uuid.NewV7().String(), iphoneLink)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// linkEnd waits until the link read leaves reading and returns it.
func (e env) linkEnd(t *testing.T, owner, id uuid.UUID) service.LinkReadView {
	t.Helper()
	var v service.LinkReadView
	testenv.Eventually(t, 30*time.Second, func() error {
		var err error
		if v, err = e.assist.GetLinkRead(context.Background(), owner, id); err != nil {
			t.Fatal(err)
		}
		if v.Status == domain.StatusReading {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return v
}
