// Package web contains the application UI bundled into the Go executable.
package web

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assets embed.FS

func Assets() fs.FS { root, _ := fs.Sub(assets, "assets"); return root }
