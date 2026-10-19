package main

import (
	"regexp"
	"strings"
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
)

func (l *Loco) key() string       { return l.ID }
func (l *Loco) setKey(id string)  { l.ID = id }
func (l *Loco) stamp(t time.Time) { l.Updated = t }

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

// locoLess sorts DCC locos by address, then analog ones, then by name.
func locoLess(a, b *Loco) bool {
	if (a.Mode == "dc") != (b.Mode == "dc") {
		return b.Mode == "dc"
	}
	if a.Address != b.Address {
		return a.Address < b.Address
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}
