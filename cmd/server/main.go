// Command server runs the TGA review web application with a local SQLite file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"tga-review/internal/app"
	"tga-review/internal/clock"
	"tga-review/internal/server"
	"tga-review/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5344", "listen address")
	dbPath := flag.String("db", "", "sqlite database path (default: ./.tga-review.db)")
	flag.Parse()

	if *dbPath == "" {
		*dbPath = filepath.Join(mustWd(), ".tga-review.db")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	svc := app.New(st, clock.Wall{})
	srv := server.New(svc)

	log.Printf("TGA review listening on http://%s (db=%s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatal(err)
	}
}

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("cwd: %v", err))
	}
	return wd
}
