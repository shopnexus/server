package embed

import (
	"fmt"
	"sync"

	// Links libonnxruntime.so (with an rpath to it) the way the speech
	// package does, so this package's own test binaries load it too.
	_ "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	ort "github.com/yalue/onnxruntime_go"
)

// The binary links libonnxruntime.so for sherpa-onnx. dlopen of the bare
// name finds that already loaded object by its soname, so both share one
// library. onnxruntime_go must stay at the release whose ORT_API_VERSION
// matches it (v1.32.1 for onnxruntime 1.28.2): a newer binding asks for an
// API the library does not have.
const ortLibrary = "libonnxruntime.so"

var (
	ortOnce sync.Once
	errORT  error
)

func initORT() error {
	ortOnce.Do(func() {
		ort.SetSharedLibraryPath(ortLibrary)
		if err := ort.InitializeEnvironment(); err != nil {
			errORT = fmt.Errorf("start onnxruntime: %w", err)
		}
	})
	return errORT
}
