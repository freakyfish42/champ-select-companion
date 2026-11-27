package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// JunglePath is a common first clear: where junglers on this champion were at 1:00, 2:00, 3:00 and 4:00 (a camp,
// a scuttle crab, a lane...), from Riot's match data on the build site (EUW, Emerald+ ranked solo, this patch).
type JunglePath struct {
	Path  []string `json:"path"`
	Games int      `json:"games"`
	Share float64  `json:"share"`
	Win   float64  `json:"win"`
}

type JungleSide struct {
	Games int          `json:"games"`
	Paths []JunglePath `json:"paths"`
}

type Jungle struct {
	Champion string      `json:"champion"`
	Patch    string      `json:"patch"`
	Blue     *JungleSide `json:"blue"`
	Red      *JungleSide `json:"red"`
}

// For: the routes for your side of the map ("blue" or "red"); the other side's if yours has none yet.
func (j *Jungle) For(side string) (*JungleSide, string) {
	if j == nil {
		return nil, ""
	}
	if side == "red" && j.Red != nil || side != "blue" && j.Blue == nil && j.Red != nil {
		return j.Red, "red"
	}
	if j.Blue != nil {
		return j.Blue, "blue"
	}
	return nil, ""
}

// FetchJungle asks the build site for a champion's common first clears.
func FetchJungle(hc *http.Client, base, champ string) (*Jungle, error) {
	req, _ := http.NewRequest("GET", base+"/api/jungle?c="+url.QueryEscape(champ), nil)
	req.Header.Set("User-Agent", "champ-select-companion")
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("the build site answered %d", res.StatusCode)
	}
	var j Jungle
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&j); err != nil {
		return nil, err
	}
	return &j, nil
}

// SideOf: Riot's team 1 is the blue side (bottom left), team 2 red.
func SideOf(team int) string {
	switch team {
	case 1:
		return "blue"
	case 2:
		return "red"
	}
	return ""
}
