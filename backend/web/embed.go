// Package web embeds the built frontend so the backend binary is
// self-contained. Build the frontend first, then `go build`.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
