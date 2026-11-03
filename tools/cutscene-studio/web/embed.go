// Package webui embeds the built editor front-end so the studio ships as one binary.
// Run `npm ci && npm run build` in this directory to refresh dist/.
//
// dist/ is build output and is not committed; only the dist/.keep placeholder is, so that
// `go build` works before the UI has ever been built. Such a binary serves the API only.
package webui

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS is the built UI, rooted at dist/. It errors when the UI has not been built.
func FS() (fs.FS, error) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, errors.New("web UI not built (run `npm ci && npm run build` in web/, then rebuild)")
	}
	return sub, nil
}
