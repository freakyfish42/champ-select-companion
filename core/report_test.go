package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendReport(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/report" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if strings.Contains(got["report"], "fail") {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":"Too many reports this hour. Try again later."}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	if err := SendReport(srv.Client(), srv.URL+"/", "Ola#EUW", "Champ Select Companion v1 · setup check"); err != nil {
		t.Fatal(err)
	}
	if got["from"] != "Ola#EUW" || !strings.HasPrefix(got["report"], "Champ Select Companion") {
		t.Fatal(got)
	}
	if err := SendReport(srv.Client(), srv.URL, "", "fail"); err == nil || !strings.Contains(err.Error(), "Too many reports") {
		t.Fatal(err)
	}
	srv.Close()
	if err := SendReport(srv.Client(), srv.URL, "", "x"); err == nil || !strings.Contains(err.Error(), "couldn't reach") {
		t.Fatal(err)
	}
}
