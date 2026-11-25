package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveLevel(t *testing.T) {
	level := `{"level":6,"currentGold":512.3,"championStats":{}}`
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if level == "" {
			w.WriteHeader(404) // loading screen: the game doesn't answer yet
			return
		}
		_, _ = w.Write([]byte(level))
	}))
	defer srv.Close()
	l := NewLiveURL(srv.URL, srv.Client())
	if n, err := l.Level(); err != nil || n != 6 {
		t.Fatal(n, err)
	}
	level = ""
	if _, err := l.Level(); err == nil {
		t.Fatal("no game yet must be an error")
	}
	level = `{"level":0}`
	if _, err := l.Level(); err == nil {
		t.Fatal("level 0 isn't a loaded game")
	}
	for _, p := range paths {
		if p != "GET /liveclientdata/activeplayer" {
			t.Fatalf("asked the game for more than your own level: %v", paths)
		}
	}
}

func TestGameWindowMode(t *testing.T) {
	cfg := "[General]\r\nCfgVersion=14.1\r\nWindowMode=2\r\nHeight=1080\r\n[HUD]\r\nWindowMode=9\r\n"
	if m, ok := GameWindowMode(cfg); !ok || m != WindowBorderless {
		t.Fatal(m, ok)
	}
	if m, ok := GameWindowMode("[General]\nwindowmode = 0\n"); !ok || m != WindowFullscreen {
		t.Fatal(m, ok)
	}
	if _, ok := GameWindowMode("[HUD]\nWindowMode=1\n"); ok {
		t.Fatal("only [General] counts")
	}
}
