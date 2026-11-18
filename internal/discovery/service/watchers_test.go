package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/discovery/service"
)

// baDinhCommune is commune 00004's point; Ngọc Hà (00008) is 2 km west of it and
// Thủ Dầu Một (25747) far south.
var (
	baDinhCommune = catalogdomain.Point{Lat: 21.0394, Lng: 105.8364}
	thuDauMot     = catalogdomain.Point{Lat: 10.9682, Lng: 106.6643}
)

func TestWatchers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ws := repository.NewWatches(e.db)
	put := func(account uuid.UUID, q string, change func(*repository.Watch)) {
		t.Helper()
		w := repository.Watch{
			ID: uuid.NewV7(), Account: account, Query: q, Phrases: []string{q}, Conditions: []string{}, Alert: true,
			CreatedAt: time.Now(),
		}
		change(&w)
		if _, _, err := ws.Put(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	seller := accounttest.New(t, e.db)
	buyer, muted, far := accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db)
	blocker, blockedBy := accounttest.New(t, e.db), accounttest.New(t, e.db)
	put(buyer, "xe đạp", func(w *repository.Watch) { w.PriceMax = 2_000_000 })
	put(buyer, "xe máy", func(*repository.Watch) {})                                      // a missing word
	put(buyer, "asama", func(w *repository.Watch) { w.Commune, w.RadiusKm = "00008", 5 }) // within 5 km
	put(far, "xe đạp", func(w *repository.Watch) { w.Commune, w.RadiusKm = "25747", 30 }) // too far
	put(muted, "xe đạp asama", func(w *repository.Watch) { w.Alert, w.OffersOnly = false, true })
	put(muted, "raw words", func(w *repository.Watch) { w.Query, w.Phrases = "Asama", []string{} })
	put(seller, "xe đạp", func(*repository.Watch) {})
	put(blocker, "xe đạp", func(*repository.Watch) {})
	put(blockedBy, "xe đạp", func(*repository.Watch) {})
	if err := e.accounts.Block(ctx, blocker, seller); err != nil {
		t.Fatal(err)
	}
	if err := e.accounts.Block(ctx, seller, blockedBy); err != nil {
		t.Fatal(err)
	}

	q := service.WatcherQuery{
		CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed, Point: baDinhCommune,
		Text: "Xe đạp Asama\nĐi ít\nKhung: nhôm",
	}
	got, err := e.discovery.Watchers(ctx, seller, q)
	if err != nil {
		t.Fatal(err)
	}
	want := []service.Watcher{{PriceMax: 2_000_000}, {}, {OffersOnly: true}, {}}
	if len(got) != len(want) {
		t.Fatalf("%+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	q.Point = thuDauMot
	if got, _ = e.discovery.Watchers(ctx, seller, q); len(got) != 4 {
		t.Fatalf("from Thủ Dầu Một the Ngọc Hà watch drops out and the Thủ Dầu Một one comes in: %+v", got)
	}
}

// A watch's area is its radius around its commune, as the sweep measures
// it: a box first, then the circle, so a draft in the box's corner is not
// counted.
func TestWatchersMeasureACircle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	buyer, seller := accounttest.New(t, e.db), accounttest.New(t, e.db)
	w := repository.Watch{
		ID: uuid.NewV7(), Account: buyer, Query: "xe đạp", Phrases: []string{"xe đạp"}, Conditions: []string{}, Alert: true,
		Commune: "00008", RadiusKm: 2, CreatedAt: time.Now(),
	}
	if _, _, err := repository.NewWatches(e.db).Put(ctx, w); err != nil {
		t.Fatal(err)
	}
	ngocHa := catalogdomain.Point{Lat: 21.0394, Lng: 105.8169}
	// 1.6 km north and 1.6 km east of Ngọc Hà: 2.26 km away, inside the 2 km box, outside the circle.
	corner := catalogdomain.Point{Lat: ngocHa.Lat + 1.6/111.32, Lng: ngocHa.Lng + 0.0154}
	q := service.WatcherQuery{CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed, Point: corner, Text: "Xe đạp"}
	got, err := e.discovery.Watchers(ctx, seller, q)
	if err != nil || len(got) != 0 {
		t.Fatalf("the corner: %+v %v", got, err)
	}
	q.Point = catalogdomain.Point{Lat: ngocHa.Lat + 1.0/111.32, Lng: ngocHa.Lng + 0.0096} // 1.4 km away
	if got, _ = e.discovery.Watchers(ctx, seller, q); len(got) != 1 {
		t.Fatalf("inside the circle: %+v", got)
	}
}
