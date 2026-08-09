// Package web embeds the built frontend static assets so the server binary
// is self-contained.
package web

import "embed"

// DistFS contains the built frontend static assets.
//
//go:embed dist
var DistFS embed.FS
