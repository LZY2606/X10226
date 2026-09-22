package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func decode(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}

func errUnknownStep(step string) error { return errors.New("unknown replay step: " + step) }

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(w, request)
	})
}

func (server *Server) ui(w http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		http.NotFound(w, request)
		return
	}
	name := "index.html"
	if request.URL.Path != "/" {
		name = strings.TrimPrefix(request.URL.Path, "/")
	}
	file, err := server.web.Open(name)
	if err != nil {
		file, err = server.web.Open("index.html")
		if err != nil {
			http.Error(w, "web UI is unavailable", http.StatusInternalServerError)
			return
		}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "web UI is unavailable", http.StatusInternalServerError)
		return
	}
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "web UI is unreadable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, request, info.Name(), time.Time{}, seeker)
}
