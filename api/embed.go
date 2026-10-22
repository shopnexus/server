// Package api holds the OpenAPI document, the source of truth for the HTTP
// contract, embedded so the binary carries the spec it validates against.
package api

import (
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path"

	"github.com/getkin/kin-openapi/openapi3"
)

// FS holds openapi.yaml and the files it references.
//
//go:embed openapi.yaml paths schemas responses parameters
var FS embed.FS

// Load reads openapi.yaml and its $refs from FS and validates the document.
func Load() (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(_ *openapi3.Loader, u *url.URL) ([]byte, error) {
		return fs.ReadFile(FS, path.Clean(u.Path))
	}
	doc, err := loader.LoadFromURI(&url.URL{Path: "openapi.yaml"})
	if err != nil {
		return nil, fmt.Errorf("load openapi.yaml: %w", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("validate openapi.yaml: %w", err)
	}
	return doc, nil
}
