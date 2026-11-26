package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// SiteURL is build.lømsland.no (punycode form for HTTP).
const SiteURL = "https://build.xn--lmsland-q1a.no"

type RuneRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

type RunePage struct {
	Primary   RuneRef   `json:"primary"`
	Secondary RuneRef   `json:"secondary"`
	Keys      []RuneRef `json:"keys"`
	Subs      []RuneRef `json:"subs"`
	Shards    []RuneRef `json:"shards"`
	Pick      *float64  `json:"pick"`
	WR        *float64  `json:"wr"`
	Play      int       `json:"play"`
}

func (p RunePage) Keystone() int {
	if len(p.Keys) == 0 {
		return 0
	}
	return p.Keys[0].ID
}

type SpellPair struct {
	IDs    []int     `json:"ids"`
	Spells []RuneRef `json:"spells"` // names (and Data Dragon images) for the ids, in the same order
	Pick   *float64  `json:"pick"`
	WR     *float64  `json:"wr"`
	Play   int       `json:"play"`
}

// Build is the part of the site's /api/build answer the app acts on. Raw keeps the whole answer for the panel.
type Build struct {
	Champion string      `json:"champion"`
	ID       string      `json:"id"`
	Position string      `json:"position"`
	Region   string      `json:"region"`
	Runes    []RunePage  `json:"runes"`
	Summs    []SpellPair `json:"summs"`
	Vs       []VsEntry   `json:"vs"` // this champion's win rate against each lane opponent
	Skills   struct {
		Order []string `json:"order"` // the point for each level, "Q" "W" "E" "R"
	} `json:"skills"`
	Raw json.RawMessage `json:"-"`
}

// FetchBuild asks the site for a champion's build. role may be "" (the champion's most played role).
func FetchBuild(hc *http.Client, base, champ, role string) (*Build, error) {
	u := base + "/api/build?c=" + url.QueryEscape(champ)
	if role != "" {
		u += "&p=" + url.QueryEscape(role)
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "champ-select-companion")
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the build site: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		var e struct{ Error string }
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("the build site answered %d", res.StatusCode)
		}
		return nil, fmt.Errorf("%s", e.Error)
	}
	var bd Build
	if err := json.Unmarshal(b, &bd); err != nil {
		return nil, fmt.Errorf("unexpected answer from the build site: %w", err)
	}
	bd.Raw = b
	return &bd, nil
}
