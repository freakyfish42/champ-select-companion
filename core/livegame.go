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

// Live reads the running game. The app asks it for one thing only: your own champion (level and skill points),
// which you can see on your own screen anyway. Nothing about other players, and it only reads.
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

// Player is what the card shows about you: your level and how many points are in Q, W, E and R
// (both on your own screen anyway).
type Player struct {
	Level  int
	Skills [4]int // Q, W, E, R
}

// Player reads your own champion. An error means the game isn't loaded yet (loading screen).
func (l *Live) Player() (Player, error) {
	resp, err := l.http.Get(l.base + "/liveclientdata/activeplayer")
	if err != nil {
		return Player{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Player{}, &HTTPError{Status: resp.StatusCode}
	}
	type ab struct {
		AbilityLevel int `json:"abilityLevel"`
	}
	var p struct {
		Level     int `json:"level"`
		Abilities struct {
			Q, W, E, R ab
		} `json:"abilities"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return Player{}, err
	}
	if p.Level < 1 || p.Level > 18 {
		return Player{}, errNotLoaded
	}
	out := Player{Level: p.Level, Skills: [4]int{p.Abilities.Q.AbilityLevel, p.Abilities.W.AbilityLevel, p.Abilities.E.AbilityLevel, p.Abilities.R.AbilityLevel}}
	for i, n := range out.Skills {
		if n < 0 || n > 6 {
			out.Skills[i] = 0
		}
	}
	return out, nil
}

// Level is your champion's level (1 to 18).
func (l *Live) Level() (int, error) {
	p, err := l.Player()
	return p.Level, err
}

// Window modes in League's game.cfg ([General] WindowMode=).
const (
	WindowFullscreen = 0
	WindowWindowed   = 1
	WindowBorderless = 2
)

// GameWindowMode reads WindowMode from League's Config/game.cfg. ok is false when it isn't there.
func GameWindowMode(cfg string) (mode int, ok bool) {
	v, found := cfgValue(cfg, "general", "WindowMode")
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// GameHUDScale reads the HUD Scale setting (0 to 1; the game's menu shows it as 0 to 100) from game.cfg.
func GameHUDScale(cfg string) (float64, bool) {
	v, found := cfgValue(cfg, "hud", "GlobalScale")
	if !found {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		return 0, false
	}
	return f, true
}

// cfgValue finds key= in [section] of an ini-style file (both case-insensitive).
func cfgValue(cfg, section, key string) (string, bool) {
	sec := ""
	sc := bufio.NewScanner(strings.NewReader(cfg))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if found && sec == section && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

var errNotLoaded = errors.New("the game isn't loaded yet")
