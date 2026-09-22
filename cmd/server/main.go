package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/example/tga-evidence/internal/app"
	"github.com/example/tga-evidence/internal/httpapi"
	"github.com/example/tga-evidence/internal/store"
	"github.com/example/tga-evidence/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5344", "HTTP listen address")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "tga-evidence.sqlite"), "SQLite database path")
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	sqliteStore, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer sqliteStore.Close()
	service := app.NewService(sqliteStore, app.SystemClock{}, 20260923)
	webFS, err := fs.Sub(web.Files, "dist")
	if err != nil {
		log.Fatal(err)
	}
	server := httpapi.New(service, webFS)
	log.Printf("TGA evidence server listening on http://%s using %s", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
