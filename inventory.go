package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Item is one line of the inventory: a track set, a loco, a car, a structure...
type Item struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Make        string    `json:"make"`
	Part        string    `json:"part"` // the maker's part or set number
	Description string    `json:"description"`
	Quantity    int       `json:"quantity"`
	Status      string    `json:"status"`    // owned, wanted, sold
	Condition   string    `json:"condition"` // new, used or blank
	Control     string    `json:"control"`   // for locos and sets: dcc, dcc-sound, analog
	Acquired    string    `json:"acquired"`  // YYYY-MM-DD, YYYY-MM or YYYY; blank if unknown
	CostCents   int       `json:"costCents"` // what was paid for this line, all of the quantity together
	Source      string    `json:"source"`    // where it came from
	Features    []string  `json:"features"`  // e.g. "Interior lights added", "Metal wheels"
	Notes       string    `json:"notes"`
	Updated     time.Time `json:"updated"`
}

var (
	categories = []string{"loco", "set", "car", "track", "structure", "electronics", "other"}
	statuses   = []string{"owned", "wanted", "sold"}
	conditions = []string{"", "new", "used"}
	controls   = []string{"", "dcc", "dcc-sound", "analog"}
	dateRe     = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)
)

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (it *Item) key() string       { return it.ID }
func (it *Item) setKey(id string)  { it.ID = id }
func (it *Item) stamp(t time.Time) { it.Updated = t }

func (it *Item) validate() error {
	it.Category = strings.ToLower(strings.TrimSpace(it.Category))
	it.Make = clip(it.Make, 40)
	it.Part = clip(it.Part, 40)
	it.Description = clip(it.Description, 120)
	it.Source = clip(it.Source, 80)
	it.Notes = clip(it.Notes, 400)
	it.Acquired = strings.TrimSpace(it.Acquired)
	if !oneOf(it.Category, categories) {
		return badInput("pick a category: " + strings.Join(categories, ", "))
	}
	if it.Make == "" && it.Part == "" && it.Description == "" {
		return badInput("give the item a maker, part number or description")
	}
	if it.Quantity < 1 || it.Quantity > 9999 {
		return badInput("quantity must be from 1 to 9999")
	}
	if it.Status == "" {
		it.Status = "owned"
	}
	if !oneOf(it.Status, statuses) {
		return badInput("status must be owned, wanted or sold")
	}
	if !oneOf(it.Condition, conditions) {
		return badInput("condition must be new, used or blank")
	}
	if !oneOf(it.Control, controls) {
		return badInput("control must be dcc, dcc-sound, analog or blank")
	}
	if it.Acquired != "" && !dateRe.MatchString(it.Acquired) {
		return badInput("write the date acquired as 2025-12-24, 2025-12 or 2025")
	}
	if it.CostCents < 0 || it.CostCents > 100_000_00 {
		return badInput("cost must be from $0 to $100,000")
	}
	seen := map[string]bool{}
	feats := it.Features[:0:0]
	for _, f := range it.Features {
		f = clip(f, 40)
		if f != "" && !seen[strings.ToLower(f)] && len(feats) < 16 {
			seen[strings.ToLower(f)] = true
			feats = append(feats, f)
		}
	}
	it.Features = feats
	return nil
}

// itemLess sorts by category, then maker, then part number.
func itemLess(a, b *Item) bool {
	ca, cb := catOrder(a.Category), catOrder(b.Category)
	if ca != cb {
		return ca < cb
	}
	if !strings.EqualFold(a.Make, b.Make) {
		return strings.ToLower(a.Make) < strings.ToLower(b.Make)
	}
	if !strings.EqualFold(a.Part, b.Part) {
		return strings.ToLower(a.Part) < strings.ToLower(b.Part)
	}
	return a.ID < b.ID
}

func catOrder(c string) int {
	for i, v := range categories {
		if v == c {
			return i
		}
	}
	return len(categories)
}

// CSV columns, in export order. Import accepts them in any order, matched by header name.
var csvCols = []string{"category", "make", "part", "description", "quantity", "status", "condition", "control", "acquired", "cost", "source", "features", "notes"}

func writeCSV(w io.Writer, items []Item) error {
	cw := csv.NewWriter(w)
	cw.Write(csvCols)
	for _, it := range items {
		cost := ""
		if it.CostCents > 0 {
			cost = fmt.Sprintf("%.2f", float64(it.CostCents)/100)
		}
		cw.Write([]string{it.Category, it.Make, it.Part, it.Description, strconv.Itoa(it.Quantity), it.Status, it.Condition, it.Control,
			it.Acquired, cost, it.Source, strings.Join(it.Features, "; "), it.Notes})
	}
	cw.Flush()
	return cw.Error()
}

// readCSV turns a spreadsheet saved as CSV into items. Only the category column is required;
// quantity defaults to 1 and status to owned.
func readCSV(r io.Reader) ([]Item, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, badInput("that file isn't a readable CSV: " + err.Error())
	}
	if len(rows) < 2 {
		return nil, badInput("the CSV has no rows under its header")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	if _, ok := col["category"]; !ok {
		return nil, badInput("the CSV needs a header row with at least a category column")
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []Item
	for n, row := range rows[1:] {
		if strings.Join(row, "") == "" {
			continue
		}
		it := Item{
			Category: get(row, "category"), Make: get(row, "make"), Part: get(row, "part"), Description: get(row, "description"),
			Status: strings.ToLower(get(row, "status")), Condition: strings.ToLower(get(row, "condition")), Control: strings.ToLower(get(row, "control")),
			Acquired: get(row, "acquired"), Source: get(row, "source"), Notes: get(row, "notes"), Quantity: 1,
		}
		if q := get(row, "quantity"); q != "" {
			f, err := strconv.ParseFloat(q, 64)
			if err != nil {
				return nil, badInput(fmt.Sprintf("row %d: quantity %q isn't a number", n+2, q))
			}
			it.Quantity = int(f)
		}
		if c := strings.NewReplacer("$", "", ",", "").Replace(get(row, "cost")); c != "" {
			f, err := strconv.ParseFloat(c, 64)
			if err != nil {
				return nil, badInput(fmt.Sprintf("row %d: cost %q isn't a number", n+2, c))
			}
			it.CostCents = int(math.Round(f * 100))
		}
		if f := get(row, "features"); f != "" {
			it.Features = strings.Split(f, ";")
		}
		if err := it.validate(); err != nil {
			return nil, badInput(fmt.Sprintf("row %d: %v", n+2, err))
		}
		out = append(out, it)
	}
	return out, nil
}
