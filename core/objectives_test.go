package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCountdown(t *testing.T) {
	for _, c := range []struct {
		t    float64
		want string
	}{
		{0, ""}, {239.9, ""}, {240, "Dragon 1:00"}, {258.3, "Dragon 0:42"}, {299.5, "Dragon 0:01"},
		{300, "Dragon is up"}, {304.9, "Dragon is up"}, {305, ""},
		{420, "Void Grubs 1:00"}, {870, "Rift Herald 0:30"}, {1199, "Baron 0:01"}, {1206, ""}, {2000, ""},
	} {
		got := ""
		if o, left, ok := Countdown(c.t); ok {
			got = CountdownText(o, left)
		}
		if got != c.want {
			t.Errorf("at %.1f s: %q, want %q", c.t, got, c.want)
		}
	}
}

func TestLiveGameTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/liveclientdata/gamestats" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"gameMode":"CLASSIC","gameTime":258.31,"mapName":"Map11","mapNumber":11,"mapTerrain":"Default"}`))
	}))
	defer srv.Close()
	if s, err := NewLiveURL(srv.URL, srv.Client()).GameTime(); err != nil || s != 258.31 {
		t.Fatal(s, err)
	}
}
