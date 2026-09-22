// Package cases embeds the repository's offline TGA case package so the demo
// and tests never touch the public network or the developer's machine.
package cases

import (
	"embed"
	"encoding/json"
	"io/fs"
)

//go:embed all:assets
var assets embed.FS

// Root returns the rooted embedded filesystem of the case package.
func Root() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// ReadFile reads an embedded case file.
func ReadFile(name string) ([]byte, error) { return fs.ReadFile(Root(), name) }

// LoadJSON decodes an embedded JSON document.
func LoadJSON(name string, v any) error {
	b, err := ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
