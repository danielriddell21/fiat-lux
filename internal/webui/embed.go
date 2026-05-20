package webui

import "embed"

// assetsFS holds the bundled HTML / JS / CSS for the web viewer.
//
//go:embed assets
var assetsFS embed.FS
