// Command server runs the TGA review application with an embedded, in-process
// SQLite database (default ./tga_review.db) and serves the synchronized review
// UI and JSON API over plain net/http.
package main

import (
	"flag"
	"log"
	"net/http"

	"tgareview/internal/clock"
	"tgareview/internal/store"
	"tgareview/internal/web"
	"tgareview/internal/webapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5344", "listen address")
	dbPath := flag.String("db", "tga_review.db", "sqlite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	api := webapi.New(st, clock.System{})
	mux := api.Routes()

	// SVG figures rendered server-side from stored artifacts.
	mux.HandleFunc("GET /api/projects/{id}/figure", func(w http.ResponseWriter, r *http.Request) {
		art, err := api.CurrentArtifact(r)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(web.Figure(art)))
	})
	mux.HandleFunc("GET /api/projects/{id}/figure/{seq}", func(w http.ResponseWriter, r *http.Request) {
		art, err := api.ArtifactAt(r)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(web.Figure(art)))
	})

	mux.Handle("GET /", http.FileServer(http.Dir("internal/web/static")))

	log.Printf("TGA review server on http://%s (db=%s)", *addr, *dbPath)
	srv := &http.Server{Addr: *addr, Handler: withLogging(mux)}
	log.Fatal(srv.ListenAndServe())
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}
