package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Loco is one locomotive on the roster.
type Loco struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Road    string    `json:"road"`
	Notes   string    `json:"notes"`
	Style   string    `json:"style"` // cartoon drawing: hood, cab, steam, electric, bullet
	Body    string    `json:"body"`  // #rrggbb
	Trim    string    `json:"trim"`  // #rrggbb
	Mode    string    `json:"mode"`  // "dcc" or "dc"
	Address int       `json:"address,omitempty"`
	Updated time.Time `json:"updated"`
}

var (
	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	styles   = map[string]bool{"hood": true, "cab": true, "steam": true, "electric": true, "bullet": true}

	errNotFound = errors.New("no loco with that id")
)

// badInput is a problem with what the page sent, as opposed to a problem saving it.
type badInput string

func (e badInput) Error() string { return string(e) }

// validate tidies a loco from the page and rejects anything that can't be saved.
func (l *Loco) validate() error {
	l.Name = clip(l.Name, 60)
	l.Road = clip(l.Road, 60)
	l.Notes = clip(l.Notes, 200)
	if l.Name == "" {
		return badInput("give the loco a name")
	}
	if !styles[l.Style] {
		l.Style = "hood"
	}
	if !hexColor.MatchString(l.Body) {
		l.Body = "#e86a1c"
	}
	if !hexColor.MatchString(l.Trim) {
		l.Trim = "#2b2b2b"
	}
	switch l.Mode {
	case "dc":
		l.Address = 0
	case "dcc":
		if l.Address < 1 || l.Address > 9999 {
			return badInput("DCC address must be from 1 to 9999")
		}
	default:
		return badInput(`mode must be "dcc" or "dc"`)
	}
	return nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// Store keeps the roster in memory and in one JSON file in the data folder.
type Store struct {
	mu    sync.Mutex
	path  string
	locos map[string]Loco
	subs  map[chan struct{}]bool
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "locos.json"), locos: map[string]Loco{}, subs: map[chan struct{}]bool{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Loco
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	for _, l := range list {
		s.locos[l.ID] = l
	}
	return s, nil
}

// List returns DCC locos by address, then analog ones, then by name.
func (s *Store) List() []Loco {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Loco {
	out := make([]Loco, 0, len(s.locos))
	for _, l := range s.locos {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Mode == "dc") != (b.Mode == "dc") {
			return b.Mode == "dc"
		}
		if a.Address != b.Address {
			return a.Address < b.Address
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// Put adds a loco (empty ID) or replaces an existing one.
func (s *Store) Put(l Loco) (Loco, error) {
	if err := l.validate(); err != nil {
		return l, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == "" {
		l.ID = newID()
	} else if _, ok := s.locos[l.ID]; !ok {
		return l, errNotFound
	}
	l.Updated = time.Now().UTC()
	old, had := s.locos[l.ID]
	s.locos[l.ID] = l
	if err := s.saveLocked(); err != nil {
		if had {
			s.locos[l.ID] = old
		} else {
			delete(s.locos, l.ID)
		}
		return l, err
	}
	s.notifyLocked()
	return l, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.locos[id]
	if !ok {
		return errNotFound
	}
	delete(s.locos, id)
	if err := s.saveLocked(); err != nil {
		s.locos[id] = old
		return err
	}
	s.notifyLocked()
	return nil
}

// saveLocked writes to a temp file and renames it, so a crash never leaves half a roster.
func (s *Store) saveLocked() error {
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

// Subscribe returns a channel that gets a signal whenever the roster changes.
func (s *Store) Subscribe() (chan struct{}, func()) {
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

func (s *Store) notifyLocked() {
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
