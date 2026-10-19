package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var errNotFound = errors.New("nothing with that id")

// badInput is a problem with what the page sent, as opposed to a problem saving it.
type badInput string

func (e badInput) Error() string { return string(e) }

// record is anything kept in a Store: a roster loco or an inventory item.
type record interface {
	key() string
	setKey(string)
	stamp(time.Time)
	validate() error
}

// Store keeps one list in memory and in one JSON file in the data folder.
type Store[T any, P interface {
	*T
	record
}] struct {
	mu   sync.Mutex
	path string
	m    map[string]T
	less func(a, b *T) bool
	subs map[chan struct{}]bool
}

func OpenStore[T any, P interface {
	*T
	record
}](dir, file string, less func(a, b *T) bool) (*Store[T, P], error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store[T, P]{path: filepath.Join(dir, file), m: map[string]T{}, less: less, subs: map[chan struct{}]bool{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []T
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	for _, v := range list {
		s.m[P(&v).key()] = v
	}
	return s, nil
}

func (s *Store[T, P]) List() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store[T, P]) listLocked() []T {
	out := make([]T, 0, len(s.m))
	for _, v := range s.m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return s.less(&out[i], &out[j]) })
	return out
}

// Put adds a record (empty id) or replaces an existing one.
func (s *Store[T, P]) Put(v T) (T, error) {
	out, err := s.PutAll([]T{v})
	if err != nil {
		return v, err
	}
	return out[0], nil
}

// PutAll saves several records in one write; if any is invalid, none are saved.
func (s *Store[T, P]) PutAll(vs []T) ([]T, error) {
	now := time.Now().UTC()
	for i := range vs {
		if err := P(&vs[i]).validate(); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range vs {
		p := P(&vs[i])
		if p.key() == "" {
			p.setKey(newID())
		} else if _, ok := s.m[p.key()]; !ok {
			return nil, errNotFound
		}
		p.stamp(now)
	}
	old := make(map[string]T, len(s.m))
	for k, v := range s.m {
		old[k] = v
	}
	for _, v := range vs {
		s.m[P(&v).key()] = v
	}
	if err := s.saveLocked(); err != nil {
		s.m = old
		return nil, err
	}
	s.notifyLocked()
	return vs, nil
}

func (s *Store[T, P]) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.m[id]
	if !ok {
		return errNotFound
	}
	delete(s.m, id)
	if err := s.saveLocked(); err != nil {
		s.m[id] = old
		return err
	}
	s.notifyLocked()
	return nil
}

// saveLocked writes to a temp file and renames it, so a crash never leaves half a file.
func (s *Store[T, P]) saveLocked() error {
	b, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Subscribe returns a channel that gets a signal whenever the list changes.
func (s *Store[T, P]) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store[T, P]) notifyLocked() {
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}
