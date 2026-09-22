// Command tga-server runs the offline TGA review application.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"tgaaudit/internal/httpapi"
	"tgaaudit/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5344", "listen address")
	dbPath := flag.String("db", "", "SQLite database path (default ./.tgaaudit.db)")
	flag.Parse()

	if *dbPath == "" {
		*dbPath = filepath.Join(".", "tgaaudit.db")
	}
	ctx := context.Background()
	st, err := store.OpenAt(ctx, *dbPath, func() time.Time { return time.Now().UTC() })
	if err != nil {
		log.Fatalf("open db %s: %v", *dbPath, err)
	}
	defer st.Close()

	webRoot := "web"
	if _, err := os.Stat(webRoot); err != nil {
		webRoot = filepath.Join("..", "..", "web")
	}
	srv := httpapi.New(st, os.DirFS(webRoot).(fs.FS))
	_ = srv.BasePath

	log.Printf("tgaaudit listening on http://%s (db=%s)", *addr, *dbPath)
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Routes()}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
	_ = fmt.Sprint
}
