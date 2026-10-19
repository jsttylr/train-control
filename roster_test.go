package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRosterAPI(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(routes(s))
	defer srv.Close()

	post := func(body string) *http.Response {
		r, err := http.Post(srv.URL+"/api/locos", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := post(`{"name":"Kato SD40-2","mode":"dcc","address":7872,"style":"hood"}`); r.StatusCode != http.StatusCreated {
		t.Fatalf("add DCC loco: %d", r.StatusCode)
	}
	if r := post(`{"name":"Old DC switcher","mode":"dc","address":55,"body":"javascript:"}`); r.StatusCode != http.StatusCreated {
		t.Fatalf("add DC loco: %d", r.StatusCode)
	}
	for _, bad := range []string{`{"name":"","mode":"dcc","address":3}`, `{"name":"x","mode":"dcc","address":0}`, `{"name":"x","mode":"dcc","address":10000}`, `{"name":"x","mode":"steam"}`, `not json`} {
		if r := post(bad); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, r.StatusCode)
		}
	}

	// Reopen from disk: the roster survives a restart, analog locos lose their address, colors are sanitized.
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 2 || list[0].Address != 7872 || list[1].Mode != "dc" || list[1].Address != 0 || list[1].Body != "#e86a1c" {
		b, _ := json.Marshal(list)
		t.Fatalf("unexpected roster: %s", b)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/locos/nope", bytes.NewBufferString(`{"name":"x","mode":"dc"}`))
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusNotFound {
		t.Errorf("update missing loco: %d", r.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/locos/"+list[0].ID, nil)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", r.StatusCode)
	}
	if len(s.List()) != 1 {
		t.Errorf("delete didn't remove the loco")
	}
}
