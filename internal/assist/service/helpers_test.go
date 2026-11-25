package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	aidomain "github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/repository"
	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/relay"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
	"github.com/shopnexus/server/internal/provider/chotot"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
	"github.com/shopnexus/server/internal/provider/llm"
)

// transcript is what the fake STT hears in a seller's video.
const transcript = "mình bán xe đạp asama giá một triệu hai có thương lượng"

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	media    *mediasvc.Media
	assist   *service.Assist
	reads    repository.ListingReads
	clock    *restatetest.Clock
	ingress  *ingress.Client
	prefix   string
	cut      *fakeCutter
	stt      *fakeSTT
	model    *fakeModel
	relay    *relay.Relay
	// catText is catalog's text model, which guesses the drafts' categories.
	catText  *embedtest.Model
	source   *fakeSource
	watchers *watchersOf
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
	catText := guessing(t)
	c := catalogtest.Service(db, accounts, m, in, p, func(d *catalogsvc.Deps) { d.Text = catText })
	e := env{
		db: db, accounts: accounts, media: m, reads: repository.NewListingReads(db),
		clock: restatetest.NewClock(time.Now()), ingress: in, prefix: p,
		cut:      &fakeCutter{frames: 3, jpeg: mediatest.JPEG(t, 96, 64)},
		stt:      &fakeSTT{text: transcript},
		model:    &fakeModel{answer: bicycleText, link: iphoneLinkText, photo: iphonePhotoText},
		relay:    relay.New(db, slog.New(slog.DiscardHandler)),
		catText:  catText,
		source:   newSource(t),
		watchers: &watchersOf{rows: []discoverysvc.Watcher{}},
	}
	e.deps = service.Deps{
		Reads: e.reads, Source: e.source, Accounts: accounts, Media: m, Catalog: c, Discovery: e.watchers,
		AI: fakeAI{e.cut, e.stt}, Model: e.model, Relay: e.relay,
		Text: embedtest.New(catalogdomain.TextDims), Image: embedtest.New(catalogdomain.PhotoDims),
		TextMin: 0.5, PhotoMin: 0.5, CallTimeout: 5 * time.Second,
		Log: slog.New(slog.DiscardHandler), Ingress: in, Prefix: p, Now: e.clock.Now,
	}
	for _, o := range opts {
		o(&e)
	}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go e.relay.Run(ctx)
	e.assist = service.New(e.deps)
	restatetest.Start(t, slices.Concat(m.Durable(), c.Durable(), e.assist.Durable())...)
	return e
}

// guessing is a text model under which the fakes' drafts read as their
// categories: each draft's text embeds as its category's name does.
func guessing(t *testing.T) *embedtest.Model {
	t.Helper()
	m := embedtest.New(catalogdomain.TextDims)
	bicycle, iphone := embedtest.Axis(catalogdomain.TextDims, 0), embedtest.Axis(catalogdomain.TextDims, 1)
	m.Set("Xe cộ › Xe đạp", bicycle)
	m.Set("Điện thoại › iPhone", iphone)
	m.Set(catalogdomain.GuessText("Xe đạp Asama xanh", "Xe đi ít, có thương lượng."), bicycle)
	m.Set(catalogdomain.GuessText("iPhone 11 64GB Trắng, pin 81%",
		"Máy dùng 3 năm, zin all, chưa sửa chữa. Pin 81%. Liên hệ."), iphone)
	m.Set(catalogdomain.GuessText("iPhone 11 Trắng", "iPhone 11 màu trắng, mặt lưng kính, cụm 2 camera sau."), iphone)
	return m
}

// video uploads a short mp4 for owner and returns its upload ID.
func (e env) video(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "video/mp4", []byte("a phone video of a bicycle"))
}

// start starts a read of a fresh video of owner's.
func (e env) start(t *testing.T, owner uuid.UUID) service.ListingReadView {
	t.Helper()
	v, err := e.assist.StartVideoRead(context.Background(), owner, uuid.NewV7(), e.video(t, owner), "")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// startHeard starts a read of a fresh video of owner's with the phone's
// transcript, under key.
func (e env) startHeard(t *testing.T, owner, key uuid.UUID, transcript string) service.ListingReadView {
	t.Helper()
	v, err := e.assist.StartVideoRead(context.Background(), owner, key, e.video(t, owner), transcript)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// end waits until the read leaves reading and returns it.
func (e env) end(t *testing.T, owner, id uuid.UUID) service.ListingReadView {
	t.Helper()
	var v service.ListingReadView
	testenv.Eventually(t, 30*time.Second, func() error {
		var err error
		if v, err = e.assist.GetRead(context.Background(), owner, id); err != nil {
			t.Fatal(err)
		}
		if v.Status == domain.StatusReading {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return v
}

// updates counts the listing_read updates of the read in owner's log.
func (e env) updates(t *testing.T, owner, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM account_updates
WHERE account_id = @account AND kind = 'listing_read' AND listing_read_id = @read`,
		pgx.StrictNamedArgs{"account": owner, "read": id}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// videoOf is the upload the read started from.
func (e env) videoOf(t *testing.T, id uuid.UUID) mediadomain.Status {
	t.Helper()
	r, err := e.reads.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := mediarepo.NewUploads(e.db).Get(context.Background(), r.Media())
	if err != nil {
		t.Fatal(err)
	}
	return u.Status()
}

// fakeAI is module ai: fakeCutter's frames and fakeSTT's speech. A read
// embeds no photo through it.
type fakeAI struct {
	*fakeCutter
	*fakeSTT
}

func (fakeAI) EmbedImages(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("a read embeds no photo through ai")
}

// fakeCutter answers as ai's Frames: frames copies of jpeg with rising
// sharpness, or err. onCall runs before each answer.
type fakeCutter struct {
	mu     sync.Mutex
	frames int
	jpeg   []byte
	err    error
	onCall func()
}

func (f *fakeCutter) Frames(_ context.Context, key string) (aidomain.FramesOutput, error) {
	if key == "" {
		return aidomain.FramesOutput{}, errors.New("no key")
	}
	f.mu.Lock()
	n, jpeg, err, onCall := f.frames, f.jpeg, f.err, f.onCall
	f.mu.Unlock()
	if onCall != nil {
		onCall()
	}
	if err != nil {
		return aidomain.FramesOutput{}, err
	}
	res := aidomain.FramesOutput{Duration: 12}
	for i := range n {
		res.Frames = append(res.Frames, aidomain.Frame{AtSeconds: float64(i) * 1.5, Sharpness: float64(100 + i), JPEG: jpeg})
	}
	return res, nil
}

// fakeSTT hears text, or fails with err, and counts its calls.
type fakeSTT struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
}

func (f *fakeSTT) Transcribe(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", errors.New("no key")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.text, f.err
}

func (f *fakeSTT) called() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeModel writes answer for a video, link for a listing and photo for a
// photo, in three pieces, or fails with err, and keeps what it was asked.
// Each piece waits for gap, so a watcher sees the text grow.
type fakeModel struct {
	mu     sync.Mutex
	answer string
	err    error
	link   string
	photo  string
	gap    time.Duration
	calls  int
	images int
	user   string
	system string
}

func (m *fakeModel) Stream(_ context.Context, r llm.Request, onText func(string)) (string, error) {
	m.mu.Lock()
	m.calls++
	m.images, m.user, m.system = len(r.Images), r.User, r.System
	answer := m.answer
	switch {
	case len(r.Images) == 0:
		answer = m.link
	case strings.HasPrefix(r.User, "The seller's photo"):
		answer = m.photo
	default:
	}
	err, gap := m.err, m.gap
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	runes := []rune(answer)
	for i := 1; i <= 6; i++ {
		time.Sleep(gap)
		onText(string(runes[:len(runes)*i/6]))
	}
	return answer, nil
}

func (m *fakeModel) asked() (calls, images int, user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.images, m.user
}

// reply is the model's JSON answer with an item.
func reply(title, description string) string {
	b, err := json.Marshal(domain.Reply{Subject: "món đồ", Found: true, Title: title, Description: description})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// noItemReply is the model's JSON answer when it sees no item.
const noItemReply = `{"subject":"một người","found":false,"title":"","description":"","reason":"Ảnh chỉ có một người."}`

// bicycleText is the model's text for the seller's bicycle video.
var bicycleText = reply("Xe đạp Asama xanh", "Xe đi ít, có thương lượng.")

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
	// imageErrs fails the photos it names, every call.
	imageErrs map[string]error
	adCalls   int
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
	if err := f.imageErrs[url]; err != nil {
		return nil, "", err
	}
	b, ok := f.images[url]
	if !ok {
		return nil, "", chotot.ErrBadImage
	}
	return b, "image/jpeg", nil
}

// iphoneLinkText is the model's text for the seller's iPhone listing; it
// leaves the phone in the description for the regex to find.
var iphoneLinkText = reply("iPhone 11 64GB Trắng, pin 81%", "Máy dùng 3 năm, zin all, chưa sửa chữa. Pin 81%. Liên hệ 0909 123 456.")

const iphoneLink = "https://www.chotot.com/tp-ho-chi-minh/quan-binh-thanh/mua-ban-dien-thoai/119482736.htm?utm_source=share"

// startLink starts a read of the fake listing as owner.
func (e env) startLink(t *testing.T, owner uuid.UUID) service.ListingReadView {
	t.Helper()
	v, err := e.assist.StartLinkRead(context.Background(), owner, uuid.NewV7(), iphoneLink)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// linkEnd waits until the link read leaves reading and returns it.
func (e env) linkEnd(t *testing.T, owner, id uuid.UUID) service.ListingReadView {
	t.Helper()
	var v service.ListingReadView
	testenv.Eventually(t, 30*time.Second, func() error {
		var err error
		if v, err = e.assist.GetRead(context.Background(), owner, id); err != nil {
			t.Fatal(err)
		}
		if v.Status == domain.StatusReading {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return v
}

// iphonePhotoText is the model's text for the seller's photo of their
// iPhone.
var iphonePhotoText = reply("iPhone 11 Trắng", "iPhone 11 màu trắng, mặt lưng kính, cụm 2 camera sau.")

// photo uploads a JPEG for owner and returns its upload ID.
func (e env) photo(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "image/jpeg", mediatest.JPEG(t, 96, 64))
}

// startPhoto starts a read of photo as owner.
func (e env) startPhoto(t *testing.T, owner, photo uuid.UUID) service.ListingReadView {
	t.Helper()
	v, err := e.assist.StartPhotoRead(context.Background(), owner, uuid.NewV7(), photo)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// watchersOf is discovery whose Watchers answers rows and keeps the query.
type watchersOf struct {
	discoverysvc.Service

	mu    sync.Mutex
	rows  []discoverysvc.Watcher
	asked discoverysvc.WatcherQuery
	who   uuid.UUID
}

func (w *watchersOf) Watchers(_ context.Context, seller uuid.UUID, q discoverysvc.WatcherQuery) ([]discoverysvc.Watcher, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.asked, w.who = q, seller
	return w.rows, nil
}
