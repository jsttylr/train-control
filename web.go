package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

//go:embed web
var webFiles embed.FS

type stores struct {
	locos *Store[Loco, *Loco]
	items *Store[Item, *Item]
}

func routes(s stores) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServerFS(static))

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	crud(mux, "/api/locos", s.locos)
	crud(mux, "/api/inventory", s.items)

	mux.HandleFunc("GET /api/inventory.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="train-inventory-%s.csv"`, time.Now().Format("2006-01-02")))
		writeCSV(w, s.items.List())
	})
	// Import adds every row of a CSV as new items, all or nothing.
	mux.HandleFunc("POST /api/inventory/import", func(w http.ResponseWriter, r *http.Request) {
		items, err := readCSV(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			writeErr(w, err)
			return
		}
		saved, err := s.items.PutAll(items)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"added": len(saved)})
	})

	// Every open page listens here and reloads when anyone changes something.
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		locoCh, doneL := s.locos.Subscribe()
		defer doneL()
		itemCh, doneI := s.items.Subscribe()
		defer doneI()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		w.Write([]byte("event: roster\ndata: {}\n\nevent: inventory\ndata: {}\n\n"))
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-locoCh:
				w.Write([]byte("event: roster\ndata: {}\n\n"))
			case <-itemCh:
				w.Write([]byte("event: inventory\ndata: {}\n\n"))
			case <-ping.C:
				w.Write([]byte(": ping\n\n"))
			}
			fl.Flush()
		}
	})
	return mux
}

// crud serves list, add, change and delete for one store.
func crud[T any, P interface {
	*T
	record
}](mux *http.ServeMux, base string, s *Store[T, P]) {
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.List())
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		var v T
		if !readJSON(w, r, &v) {
			return
		}
		P(&v).setKey("")
		save(w, s, v, http.StatusCreated)
	})
	mux.HandleFunc("PUT "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v T
		if !readJSON(w, r, &v) {
			return
		}
		P(&v).setKey(r.PathValue("id"))
		save(w, s, v, http.StatusOK)
	})
	mux.HandleFunc("DELETE "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.PathValue("id")); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func save[T any, P interface {
	*T
	record
}](w http.ResponseWriter, s *Store[T, P], v T, status int) {
	out, err := s.Put(v)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, status, out)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "couldn't read what the page sent"})
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
