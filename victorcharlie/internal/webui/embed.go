// Package webui holds the built web app. build.sh fills dist/ from web/.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func FS() fs.FS {
	sub, _ := fs.Sub(files, "dist")
	return sub
}
