package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"time"
)

//go:embed web
var webFiles embed.FS

func routes(s *Store) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServerFS(static))

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/locos", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.List())
	})
	mux.HandleFunc("POST /api/locos", func(w http.ResponseWriter, r *http.Request) {
		var l Loco
		if !readJSON(w, r, &l) {
			return
		}
		l.ID = ""
		save(w, s, l, http.StatusCreated)
	})
	mux.HandleFunc("PUT /api/locos/{id}", func(w http.ResponseWriter, r *http.Request) {
		var l Loco
		if !readJSON(w, r, &l) {
			return
		}
		l.ID = r.PathValue("id")
		save(w, s, l, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/locos/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.PathValue("id")); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Every open page listens here and reloads the roster when anyone changes it.
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch, done := s.Subscribe()
		defer done()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		w.Write([]byte("event: roster\ndata: {}\n\n"))
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ch:
				w.Write([]byte("event: roster\ndata: {}\n\n"))
			case <-ping.C:
				w.Write([]byte(": ping\n\n"))
			}
			fl.Flush()
		}
	})
	return mux
}

func save(w http.ResponseWriter, s *Store, l Loco, status int) {
	out, err := s.Put(l)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, status, out)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "couldn't read that loco"})
		return false
	}
	return true
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var bad badInput
	if errors.Is(err, errNotFound) {
		status = http.StatusNotFound
	} else if errors.As(err, &bad) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
