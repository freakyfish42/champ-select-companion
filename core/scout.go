package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
)

// Roles in the site's order.
var Roles = []string{"top", "jungle", "mid", "adc", "support"}

// Norm matches the site's champion name matching: lower case, letters and digits only ("Kai'Sa" → "kaisa").
func Norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- the site's tier list: how often each champion is played in each role ----

type TierEntry struct {
	Name string  `json:"name"`
	ID   string  `json:"id"`
	Play int     `json:"play"`
	Ban  float64 `json:"ban"` // ban rate, 0..1 (EUW Emerald+)
}

type TierList struct {
	Roles   map[string][]TierEntry `json:"roles"`
	OpggAt  int64                  `json:"opggAt"`  // when op.gg last processed its stats (ms)
	Updated int64                  `json:"updated"` // when the site fetched them (ms)
	share   map[string]map[string]float64
	champ   map[string]TierEntry
}

func FetchTierList(hc *http.Client, base string) (*TierList, error) {
	req, _ := http.NewRequest("GET", base+"/api/tierlist", nil)
	req.Header.Set("User-Agent", "champ-select-companion")
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("tier list: %s", res.Status)
	}
	var t TierList
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&t); err != nil {
		return nil, err
	}
	t.index()
	return &t, nil
}

func (t *TierList) index() {
	t.share, t.champ = map[string]map[string]float64{}, map[string]TierEntry{}
	total := map[string]int{}
	for _, r := range Roles {
		for _, e := range t.Roles[r] {
			total[Norm(e.ID)] += e.Play
		}
	}
	for _, r := range Roles {
		for _, e := range t.Roles[r] {
			k := Norm(e.ID)
			if total[k] == 0 {
				continue
			}
			for _, key := range []string{k, Norm(e.Name)} {
				if t.share[key] == nil {
					t.share[key] = map[string]float64{}
				}
				t.share[key][r] = float64(e.Play) / float64(total[k])
				t.champ[key] = TierEntry{Name: e.Name, ID: e.ID}
			}
		}
	}
}

// Share is how much of a champion's games are in a role (0..1), by id or name.
func (t *TierList) Share(champ, role string) float64 {
	if t == nil || t.share == nil {
		return 0
	}
	return t.share[Norm(champ)][role]
}

// Champ finds a champion's display name and Data Dragon id.
func (t *TierList) Champ(key string) (TierEntry, bool) {
	if t == nil || t.champ == nil {
		return TierEntry{}, false
	}
	e, ok := t.champ[Norm(key)]
	return e, ok
}

// AssignRoles gives each of up to five entries a different role, the most likely overall.
// w[i][r] is how likely entry i plays Roles[r]; fixed[i] is a role already known ("" = guess).
func AssignRoles(w [][5]float64, fixed []string) []string {
	n := len(w)
	out := make([]string, n)
	if n == 0 || n > 5 {
		return out
	}
	best, bestScore := []int(nil), math.Inf(-1)
	perm := make([]int, 0, n)
	used := [5]bool{}
	var try func(i int, score float64)
	try = func(i int, score float64) {
		if i == n {
			if score > bestScore {
				best, bestScore = append([]int{}, perm...), score
			}
			return
		}
		for r := 0; r < 5; r++ {
			if used[r] || (i < len(fixed) && fixed[i] != "" && fixed[i] != Roles[r]) {
				continue
			}
			used[r] = true
			perm = append(perm, r)
			try(i+1, score+math.Log(w[i][r]+0.01))
			perm = perm[:len(perm)-1]
			used[r] = false
		}
	}
	try(0, 0)
	for i, r := range best {
		out[i] = Roles[r]
	}
	return out
}

// ---- lane matchup ----

type VsEntry struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	WR   float64 `json:"wr"`
	Play int     `json:"play"`
}

// Matchup is the panel's line about the lane opponent.
type Matchup struct {
	State string   `json:"state"` // waiting (their laner hasn't picked), ok, nodata
	Role  string   `json:"role"`
	Opp   string   `json:"opp,omitempty"`
	OppID string   `json:"oppId,omitempty"`
	Sure  bool     `json:"sure"` // the role guess is clear (the champion is almost only played there)
	WR    *float64 `json:"wr,omitempty"`
	Play  int      `json:"play,omitempty"`
}

// EnemyIn guesses which enemy pick plays role: the most likely way to share out the roles, from how often each
// champion is played where (the site's tier list), keeping roles that are known (Clash positions). i is -1 when
// nobody fits; sure says the guess is clear (known, or the champion is almost only played there).
func EnemyIn(role string, enemies []Champion, known []string, t *TierList) (i int, sure bool) {
	if role == "" || t == nil || len(enemies) == 0 {
		return -1, false
	}
	if len(enemies) > 5 {
		enemies = enemies[:5]
	}
	w := make([][5]float64, len(enemies))
	for i, e := range enemies {
		for r, ro := range Roles {
			w[i][r] = t.Share(e.Alias, ro)
		}
	}
	fixed := make([]string, len(enemies))
	copy(fixed, known)
	for i, r := range AssignRoles(w, fixed) {
		if r == role {
			return i, fixed[i] != "" || t.Share(enemies[i].Alias, r) >= 0.6
		}
	}
	return -1, false
}

// EnemyRoles guesses every enemy pick's role at once (see EnemyIn); sure[i] says the guess is clear.
func EnemyRoles(enemies []Champion, known []string, t *TierList) (roles []string, sure []bool) {
	if t == nil || len(enemies) == 0 || len(enemies) > 5 {
		return make([]string, len(enemies)), make([]bool, len(enemies))
	}
	w := make([][5]float64, len(enemies))
	for i, e := range enemies {
		for r, ro := range Roles {
			w[i][r] = t.Share(e.Alias, ro)
		}
	}
	fixed := make([]string, len(enemies))
	copy(fixed, known)
	roles = AssignRoles(w, fixed)
	sure = make([]bool, len(enemies))
	for i, r := range roles {
		sure[i] = r != "" && (fixed[i] != "" || t.Share(enemies[i].Alias, r) >= 0.6)
	}
	return roles, sure
}

// LaneMatchup guesses which enemy pick plays myRole and looks up this build's win rate against it.
// known[i] is enemies[i]'s role when it's known (Clash positions), else "".
func LaneMatchup(b *Build, myRole string, enemies []Champion, known []string, t *TierList) Matchup {
	m := Matchup{State: "waiting", Role: myRole}
	i, sure := EnemyIn(myRole, enemies, known, t)
	if i < 0 {
		return m
	}
	e := enemies[i]
	m.Opp, m.OppID, m.State, m.Sure = e.Name, e.Alias, "nodata", sure
	for _, v := range b.Vs {
		if Norm(v.ID) == Norm(e.Alias) || Norm(v.Name) == Norm(e.Name) {
			wr := v.WR
			m.WR, m.Play, m.State = &wr, v.Play, "ok"
		}
	}
	return m
}

// ---- Clash scouting, from the team's Clash helper (clash.lømsland.no) ----

const ClashURL = "https://clash.xn--lmsland-q1a.no"

type ClashMatchInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Updated int64  `json:"updated"`
}

type ClashChamp struct {
	K      string  `json:"k"`
	G      float64 `json:"g"`
	WR     float64 `json:"wr"` // percent
	RG     float64 `json:"rg"`
	Season bool    `json:"season"`
}

type ClashPlayer struct {
	ID     string       `json:"id"`
	Role   string       `json:"role"` // T J M B S, or "" when not set
	Champs []ClashChamp `json:"champs"`
}

type ClashMatch struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Updated int64         `json:"updated"`
	Them    []ClashPlayer `json:"them"`
}

func clashPost(hc *http.Client, base string, body, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/api/store", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "champ-select-companion")
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the Clash helper: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode != 200 {
		var e struct{ Error string }
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = "the Clash helper answered " + res.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	return json.Unmarshal(raw, out)
}

func ClashMatches(hc *http.Client, base string) ([]ClashMatchInfo, error) {
	var r struct {
		Matches []ClashMatchInfo `json:"matches"`
	}
	err := clashPost(hc, base, map[string]string{"op": "list"}, &r)
	sort.SliceStable(r.Matches, func(i, j int) bool { return r.Matches[i].Updated > r.Matches[j].Updated })
	return r.Matches, err
}

func GetClashMatch(hc *http.Client, base, id string) (*ClashMatch, error) {
	var r struct {
		Match *ClashMatch `json:"match"`
	}
	if err := clashPost(hc, base, map[string]string{"op": "get", "id": id}, &r); err != nil {
		return nil, err
	}
	if r.Match == nil {
		return nil, fmt.Errorf("that match isn't saved any more")
	}
	return r.Match, nil
}

var clashRole = map[string]string{"T": "top", "J": "jungle", "M": "mid", "B": "adc", "S": "support"}

type ScoutChamp struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Games int     `json:"games"`
	WR    float64 `json:"wr"` // 0..1
}

type ScoutPlayer struct {
	Name    string       `json:"name"`
	Role    string       `json:"role"`
	Guessed bool         `json:"guessed"`
	Champs  []ScoutChamp `json:"champs"`
}

type ScoutBan struct {
	ScoutChamp
	Player string `json:"player"`
}

type Scout struct {
	MatchID string        `json:"matchId"`
	Match   string        `json:"match"`
	Updated int64         `json:"updated"`
	Players []ScoutPlayer `json:"players"`
	Bans    []ScoutBan    `json:"bans"`
}

func (c ClashChamp) games() float64 {
	if c.Season {
		return c.G + c.RG
	}
	return c.G
}

// ScoutTeam turns a saved Clash match into the enemy roles (saved, or guessed from their champions),
// each player's three most played champions in that role, and three ban ideas.
func ScoutTeam(m *ClashMatch, t *TierList) Scout {
	s := Scout{MatchID: m.ID, Match: m.Name, Updated: m.Updated}
	var them []ClashPlayer
	for _, p := range m.Them {
		if strings.TrimSpace(p.ID) != "" || len(p.Champs) > 0 {
			them = append(them, p)
		}
	}
	if len(them) > 5 {
		them = them[:5]
	}
	w := make([][5]float64, len(them))
	fixed := make([]string, len(them))
	for i, p := range them {
		fixed[i] = clashRole[strings.ToUpper(p.Role)]
		tot := 0.0
		for _, c := range p.Champs {
			g := c.games()
			tot += g
			for r, role := range Roles {
				w[i][r] += g * t.Share(c.K, role)
			}
		}
		if tot > 0 {
			for r := range w[i] {
				w[i][r] /= tot
			}
		}
	}
	roles := AssignRoles(w, fixed)
	for i := range them {
		if fixed[i] == "" && w[i] == ([5]float64{}) {
			roles[i] = "" // nothing to guess from
		}
	}
	type cand struct {
		ScoutBan
		score float64
	}
	var cands []cand
	for i, p := range them {
		sp := ScoutPlayer{Name: p.ID, Role: roles[i], Guessed: fixed[i] == ""}
		if sp.Name == "" {
			sp.Name = fmt.Sprintf("Player %d", i+1)
		}
		cs := append([]ClashChamp{}, p.Champs...)
		fit := func(c ClashChamp) bool { return roles[i] == "" || t.Share(c.K, roles[i]) >= 0.25 }
		sort.SliceStable(cs, func(a, b int) bool {
			if fit(cs[a]) != fit(cs[b]) {
				return fit(cs[a])
			}
			return cs[a].games() > cs[b].games()
		})
		for _, c := range cs {
			if c.games() <= 0 {
				continue
			}
			e, ok := t.Champ(c.K)
			if !ok {
				e = TierEntry{Name: c.K, ID: ""}
			}
			sc := ScoutChamp{ID: e.ID, Name: e.Name, Games: int(c.games()), WR: c.WR / 100}
			if len(sp.Champs) < 3 {
				sp.Champs = append(sp.Champs, sc)
			}
			if fit(c) && c.games() >= 5 {
				// comfort picks that win: many games, above-average win rate
				cands = append(cands, cand{ScoutBan{sc, sp.Name}, math.Log2(1+c.games()) * (sc.WR / 0.5)})
			}
		}
		s.Players = append(s.Players, sp)
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].score > cands[b].score })
	seen := map[string]bool{}
	for _, c := range cands {
		if len(s.Bans) == 3 {
			break
		}
		if !seen[Norm(c.Name)] {
			seen[Norm(c.Name)] = true
			s.Bans = append(s.Bans, c.ScoutBan)
		}
	}
	// list players top to support
	order := map[string]int{"top": 0, "jungle": 1, "mid": 2, "adc": 3, "support": 4, "": 5}
	sort.SliceStable(s.Players, func(a, b int) bool { return order[s.Players[a].Role] < order[s.Players[b].Role] })
	return s
}

// Counters turns the lane opponent's build into pick ideas against them: the champions the opponent
// does worst against in that lane (40+ games each), with your win rate (1 - theirs), best first.
func Counters(opp *Build, n int) []VsEntry {
	if opp == nil {
		return nil
	}
	list := append([]VsEntry{}, opp.Vs...)
	sort.SliceStable(list, func(a, b int) bool { return list[a].WR < list[b].WR })
	var out []VsEntry
	for _, v := range list {
		if len(out) == n {
			break
		}
		if v.WR >= 0.5 {
			break // only champions that actually beat them
		}
		out = append(out, VsEntry{ID: v.ID, Name: v.Name, WR: math.Round((1-v.WR)*10000) / 10000, Play: v.Play})
	}
	return out
}

// BanRate is one champion in the most-banned list.
type BanRate struct {
	ID   string  `json:"id"` // Data Dragon id, e.g. "MonkeyKing"
	Name string  `json:"name"`
	Ban  float64 `json:"ban"`
}

// TopBans: the n champions banned most often (EUW Emerald+, from the site's tier list). A champion appears in
// several roles with the same ban rate; it is listed once.
func (t *TierList) TopBans(n int) []BanRate {
	if t == nil {
		return nil
	}
	best := map[string]BanRate{}
	for _, list := range t.Roles {
		for _, e := range list {
			if e.ID == "" || e.Ban <= 0 {
				continue
			}
			if b, ok := best[e.ID]; !ok || e.Ban > b.Ban {
				best[e.ID] = BanRate{ID: e.ID, Name: e.Name, Ban: e.Ban}
			}
		}
	}
	out := make([]BanRate, 0, len(best))
	for _, b := range best {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ban != out[j].Ban {
			return out[i].Ban > out[j].Ban
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
