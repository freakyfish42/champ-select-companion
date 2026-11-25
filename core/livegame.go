package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LiveURL is Riot's Live Client Data API: the running game serves it on the player's own PC.
const LiveURL = "https://127.0.0.1:2999"

// Live reads the running game. The app asks it for one thing only: your own champion's level, which you
// can see on your own screen anyway. Nothing about other players, and it only reads.
type Live struct {
	base string
	http *http.Client
}

func NewLive() *Live {
	return NewLiveURL(LiveURL, &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{
		Proxy: nil, TLSClientConfig: RiotTLS(), MaxIdleConns: 1, IdleConnTimeout: 30 * time.Second,
	}})
}

func NewLiveURL(base string, hc *http.Client) *Live {
	return &Live{base: strings.TrimRight(base, "/"), http: hc}
}

// Level is your champion's level (1 to 18). An error means the game isn't loaded yet (loading screen).
func (l *Live) Level() (int, error) {
	resp, err := l.http.Get(l.base + "/liveclientdata/activeplayer")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, &HTTPError{Status: resp.StatusCode}
	}
	var p struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return 0, err
	}
	if p.Level < 1 || p.Level > 18 {
		return 0, errNotLoaded
	}
	return p.Level, nil
}

// Window modes in League's game.cfg ([General] WindowMode=).
const (
	WindowFullscreen = 0
	WindowWindowed   = 1
	WindowBorderless = 2
)

// GameWindowMode reads WindowMode from League's Config/game.cfg. ok is false when it isn't there.
func GameWindowMode(cfg string) (mode int, ok bool) {
	sec := ""
	sc := bufio.NewScanner(strings.NewReader(cfg))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if found && sec == "general" && strings.EqualFold(strings.TrimSpace(k), "WindowMode") {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

var errNotLoaded = errors.New("the game isn't loaded yet")
