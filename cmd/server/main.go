// Command tga-server serves the TGA review system over HTTP. All data is held
// in a local SQLite file in a temporary working directory by default; pass
// -db to keep it elsewhere. No network is used except for this listener.
package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"tga-archive/internal/app"
	"tga-archive/internal/httpapi"
	"tga-archive/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5344", "listen address")
	dbPath := flag.String("db", filepath.Join(".", "tga-archive.db"), "SQLite database path")
	flag.Parse()

	clock := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	st, err := store.Open(*dbPath, clock)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	a := app.New(st)
	srv := httpapi.New(a)
	log.Printf("TGA archive listening on http://%s (db=%s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
