//go:build measure

// Compares each variant folder in $AI_TEST_SIGLIP_DIRS (comma-separated,
// absolute, each holding siglip2-so400m-patch16-384/) with the reference:
//
//	AI_TEST_SIGLIP_DIRS=$PWD/tmp/measure/fp32,$PWD/tmp/measure/int8 go test -tags measure -run TestMeasure -v ./embed/
package embed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeasure(t *testing.T) {
	ref := loadReference(t)
	for dir := range strings.SplitSeq(os.Getenv("AI_TEST_SIGLIP_DIRS"), ",") {
		e, err := New(Config{Models: map[string]string{"m": filepath.Join(dir, "siglip2-so400m-patch16-384")}, NumThreads: 4, MaxConcurrent: 1})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		imgs, txts := embedReference(t, e, ref)
		perInput := time.Since(start) / time.Duration(len(imgs)+len(txts))
		minCos := 1.0
		for i, v := range imgs {
			minCos = min(minCos, cosine(v, ref.Images[i].Embedding))
		}
		for i, v := range txts {
			minCos = min(minCos, cosine(v, ref.Texts[i].Embedding))
		}
		t.Logf("%s: min cosine %.5f, rankings kept %v, %v per input", dir, minCos, sameRankings(imgs, txts, ref), perInput)
		e.Close()
	}
}
