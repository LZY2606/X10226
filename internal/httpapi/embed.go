package httpapi

import "embed"

//go:embed web/app.css web/app.js
var staticFS embed.FS
