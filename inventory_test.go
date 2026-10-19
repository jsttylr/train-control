package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	locos, _ := OpenStore[Loco](dir, "locos.json", locoLess)
	items, err := OpenStore[Item](dir, "inventory.json", itemLess)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(routes(stores{locos, items}))
	defer srv.Close()

	in := "\ufeffCategory,Make,Part,Quantity,Cost,Features,Acquired,Control\n" +
		"track,Kato,20-860,4,\"$1,024.50\",Power feeder; ,2024-12,\n" +
		"loco,Atlas,40005462,1,189.99,Decoder installed;Sound,2025,dcc-sound\n" +
		",,,,,,,\n"
	r, err := http.Post(srv.URL+"/api/inventory/import", "text/csv", strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]int
	json.NewDecoder(r.Body).Decode(&res)
	if r.StatusCode != http.StatusOK || res["added"] != 2 {
		t.Fatalf("import: %d %v", r.StatusCode, res)
	}
	list := items.List()
	if list[0].Category != "loco" || list[1].CostCents != 102450 || list[1].Quantity != 4 || len(list[1].Features) != 1 || list[1].Status != "owned" {
		b, _ := json.Marshal(list)
		t.Fatalf("unexpected inventory: %s", b)
	}

	// A bad row rejects the whole import, so nothing is half-added.
	r, _ = http.Post(srv.URL+"/api/inventory/import", "text/csv", strings.NewReader("category,quantity\ncar,2\nspaceship,1\n"))
	if r.StatusCode != http.StatusBadRequest || len(items.List()) != 2 {
		t.Fatalf("bad import: %d, %d items", r.StatusCode, len(items.List()))
	}

	for _, bad := range []string{`{"category":"car","quantity":0,"make":"x"}`, `{"category":"car","quantity":1}`, `{"category":"car","quantity":1,"make":"x","acquired":"Dec 2024"}`, `{"category":"car","quantity":1,"make":"x","costCents":-5}`} {
		r, _ := http.Post(srv.URL+"/api/inventory", "application/json", bytes.NewBufferString(bad))
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, r.StatusCode)
		}
	}

	// Export and re-import gives the same lines back.
	r, _ = http.Get(srv.URL + "/api/inventory.csv")
	out, _ := io.ReadAll(r.Body)
	again, err := readCSV(bytes.NewReader(out))
	if err != nil || len(again) != 2 || again[1].CostCents != 102450 || again[0].Features[1] != "Sound" {
		t.Fatalf("round trip: %v %+v\n%s", err, again, out)
	}
}
