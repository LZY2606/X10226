package web

import "embed"

//go:embed dist/index.html dist/app.js dist/styles.css
var Files embed.FS
