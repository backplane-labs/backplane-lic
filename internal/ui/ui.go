// Package ui carries the console module built by make web. Without a
// bundle the binary exposes the API but declares no console UI.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// Bundle returns the embedded plugin, or nil in a backend-only build.
func Bundle() fs.FS {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}

	if _, err := fs.Stat(dist, "plugin.json"); err != nil {
		return nil
	}

	return dist
}
