package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchJungle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/jungle" || r.URL.Query().Get("c") != "MonkeyKing" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"champion":"MonkeyKing","patch":"16.23","blue":{"games":40,"win":0.5,"start":[],"paths":[{"path":["Red","Raptors","Blue","Top scuttle"],"games":12,"share":0.3,"win":0.55}]},"red":null}`))
	}))
	defer srv.Close()
	j, err := FetchJungle(srv.Client(), srv.URL, "MonkeyKing")
	if err != nil || j.Blue == nil || len(j.Blue.Paths) != 1 || j.Blue.Paths[0].Path[3] != "Top scuttle" {
		t.Fatal(j, err)
	}
	if s, side := j.For("red"); s != j.Blue || side != "blue" {
		t.Error("red side has no data yet: show blue's, and say so")
	}
	if s, side := j.For("blue"); s != j.Blue || side != "blue" {
		t.Error("blue")
	}
	if s, _ := (*Jungle)(nil).For("blue"); s != nil {
		t.Error("nil")
	}
	if SideOf(1) != "blue" || SideOf(2) != "red" || SideOf(0) != "" {
		t.Error("sides")
	}
}
