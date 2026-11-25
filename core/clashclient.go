package core

// Reading this round's Clash opponent from the League client itself (the data its own scouting tab shows).
//
// The client's /lol-clash endpoints aren't documented by Riot and their exact shapes haven't been seen yet,
// so everything here reads leniently: ids are found by key name anywhere in the answer, numbers may be
// strings, and every raw answer is handed to a logger so the real format can be checked after a Clash.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// RawLog receives each raw client answer during a Clash, for checking the real formats later.
type RawLog func(path string, body []byte, err error)

func (c *Client) getRaw(path string, log RawLog) (any, error) {
	var v any
	err := c.Do("GET", path, nil, &v)
	if log != nil {
		b, _ := json.Marshal(v)
		log(path, b, err)
	}
	return v, err
}

// idString turns a JSON id (string or number) into a string.
func idString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
	}
	return ""
}

func numOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func firstNum(o map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		for ok, v := range o {
			if strings.EqualFold(ok, k) {
				if n, good := numOf(v); good {
					return n, true
				}
			}
		}
	}
	return 0, false
}

func firstStr(o map[string]any, keys ...string) string {
	for _, k := range keys {
		for ok, v := range o {
			if strings.EqualFold(ok, k) {
				if s := idString(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// walk calls f for every object in a JSON value.
func walk(v any, f func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		f(x)
		for _, c := range x {
			walk(c, f)
		}
	case []any:
		for _, c := range x {
			walk(c, f)
		}
	}
}

// rosterIDs finds every value under a key named like "rosterId" (or the id of a "roster" object).
func rosterIDs(v any) []string {
	var out []string
	walk(v, func(o map[string]any) {
		for k, val := range o {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "rosterid") {
				switch arr := val.(type) {
				case []any:
					for _, x := range arr {
						if s := idString(x); s != "" {
							out = append(out, s)
						}
					}
				default:
					if s := idString(val); s != "" && s != "0" {
						out = append(out, s)
					}
				}
			}
			if lk == "roster" || lk == "currentroster" {
				if r, ok := val.(map[string]any); ok {
					if s := firstStr(r, "id"); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	})
	return out
}

// ClashMember is one player on a Clash roster.
type ClashMember struct {
	Puuid      string
	SummonerID string
	Name       string
	Position   string // top, jungle, mid, adc, support, or "" (fill / not set)
}

type clashRoster struct {
	ID, Name, BracketID string
	Members             []ClashMember
}

func clashPos(p string) string {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "TOP":
		return "top"
	case "JUNGLE":
		return "jungle"
	case "MIDDLE", "MID":
		return "mid"
	case "BOTTOM", "BOT", "ADC":
		return "adc"
	case "UTILITY", "SUPPORT":
		return "support"
	}
	return ""
}

func (c *Client) roster(id string, log RawLog) (*clashRoster, error) {
	v, err := c.getRaw("/lol-clash/v1/roster/"+url.PathEscape(id), log)
	if err != nil {
		return nil, err
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("unexpected roster format")
	}
	r := &clashRoster{ID: id, Name: firstStr(o, "name"), BracketID: firstStr(o, "bracketId")}
	ms, _ := o["members"].([]any)
	if ms == nil {
		for k, val := range o { // other spellings of "members"
			if strings.Contains(strings.ToLower(k), "member") {
				ms, _ = val.([]any)
			}
		}
	}
	for _, m := range ms {
		mo, ok := m.(map[string]any)
		if !ok {
			continue
		}
		cm := ClashMember{Puuid: firstStr(mo, "puuid"), SummonerID: firstStr(mo, "summonerId"), Position: clashPos(firstStr(mo, "position"))}
		name, tag := firstStr(mo, "gameName", "riotIdGameName"), firstStr(mo, "tagLine", "riotIdTagLine")
		if name == "" {
			name = firstStr(mo, "summonerName", "displayName", "name")
		}
		if name != "" && tag != "" {
			name += "#" + tag
		}
		cm.Name = name
		if cm.Puuid != "" || cm.SummonerID != "" || cm.Name != "" {
			r.Members = append(r.Members, cm)
		}
	}
	return r, nil
}

// ClashOpponent finds this round's opposing team: our roster → its bracket → the open match → the other roster.
func (c *Client) ClashOpponent(myPuuid string, log RawLog) (*clashRoster, error) {
	var cands []string
	for _, p := range []string{"/lol-clash/v1/player", "/lol-clash/v1/thirdparty/team-data", "/lol-clash/v1/tournament-summary"} {
		if v, err := c.getRaw(p, log); err == nil {
			cands = append(cands, rosterIDs(v)...)
		}
	}
	seen := map[string]bool{}
	var mine *clashRoster
	for _, id := range cands {
		if seen[id] {
			continue
		}
		seen[id] = true
		r, err := c.roster(id, log)
		if err != nil {
			continue
		}
		for _, m := range r.Members {
			if myPuuid != "" && m.Puuid == myPuuid {
				mine = r
			}
		}
		if mine != nil {
			break
		}
		if mine == nil && myPuuid == "" && r.BracketID != "" {
			mine = r
			break
		}
	}
	if mine == nil {
		return nil, errors.New("the client didn't show your Clash team")
	}
	if mine.BracketID == "" {
		return nil, errors.New("your Clash team has no bracket yet")
	}
	bv, err := c.getRaw("/lol-clash/v1/bracket/"+url.PathEscape(mine.BracketID), log)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the bracket: %w", err)
	}
	// the match with our team that has no winner yet, latest round first
	type match struct {
		round    float64
		opponent string
		done     bool
	}
	var ms []match
	walk(bv, func(o map[string]any) {
		r1, r2 := firstStr(o, "rosterId1"), firstStr(o, "rosterId2")
		if r1 == "" && r2 == "" {
			return
		}
		var opp string
		switch mine.ID {
		case r1:
			opp = r2
		case r2:
			opp = r1
		default:
			return
		}
		round, _ := firstNum(o, "roundId", "round")
		w := firstStr(o, "winnerId")
		ms = append(ms, match{round, opp, w != "" && w != "0"})
	})
	sort.SliceStable(ms, func(a, b int) bool { return ms[a].round > ms[b].round })
	opp := ""
	for _, m := range ms {
		if !m.done && m.opponent != "" && m.opponent != "0" {
			opp = m.opponent
			break
		}
	}
	if opp == "" {
		return nil, errors.New("no opponent in the bracket yet")
	}
	r, err := c.roster(opp, log)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the other team: %w", err)
	}
	if len(r.Members) == 0 {
		return nil, errors.New("the other team's players aren't visible yet")
	}
	return r, nil
}

type champStat struct{ games, wins float64 }

// scoutingChampions reads the client's scouting champions for the given players, trying the likely ways
// of asking. It returns champion stats per player (keyed by puuid or summoner id).
func (c *Client) scoutingChampions(ms []ClashMember, log RawLog) map[string]map[int]*champStat {
	var puuids, sids []string
	for _, m := range ms {
		if m.Puuid != "" {
			puuids = append(puuids, strconv.Quote(m.Puuid))
		}
		if m.SummonerID != "" {
			sids = append(sids, m.SummonerID)
		}
	}
	var tries []string
	if len(sids) > 0 {
		tries = append(tries, "?summonerIds="+url.QueryEscape("["+strings.Join(sids, ",")+"]"), "?summonerIds="+url.QueryEscape(strings.Join(sids, ",")))
	}
	if len(puuids) > 0 {
		tries = append(tries, "?puuids="+url.QueryEscape("["+strings.Join(puuids, ",")+"]"))
	}
	tries = append(tries, "")
	for _, q := range tries {
		v, err := c.getRaw("/lol-clash/v1/scouting/champions"+q, log)
		if err != nil {
			continue
		}
		out := parseScoutChamps(v, ms)
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// parseScoutChamps finds champion stats in any shape: objects with a championId, under the nearest
// object that names a player (puuid or summonerId), or under a map keyed by the player's id.
func parseScoutChamps(v any, ms []ClashMember) map[string]map[int]*champStat {
	known := map[string]bool{}
	for _, m := range ms {
		for _, k := range []string{m.Puuid, m.SummonerID} {
			if k != "" {
				known[k] = true
			}
		}
	}
	out := map[string]map[int]*champStat{}
	var rec func(node any, owner string)
	rec = func(node any, owner string) {
		switch x := node.(type) {
		case map[string]any:
			if p := firstStr(x, "puuid", "summonerId"); known[p] {
				owner = p
			}
			if id, ok := firstNum(x, "championId"); ok && owner != "" && id > 0 {
				g, _ := firstNum(x, "gameCount", "games", "totalGames", "numGames", "count", "played", "matches")
				w, hasW := firstNum(x, "winCount", "wins", "win")
				if !hasW {
					if r, ok := firstNum(x, "winRate", "winrate"); ok {
						if r > 1 {
							r /= 100
						}
						w = r * g
					}
				}
				if out[owner] == nil {
					out[owner] = map[int]*champStat{}
				}
				s := out[owner][int(id)]
				if s == nil {
					s = &champStat{}
					out[owner][int(id)] = s
				}
				if g == 0 {
					g = 1
				}
				s.games += g
				s.wins += w
			}
			for k, c := range x {
				o := owner
				if known[k] {
					o = k
				}
				rec(c, o)
			}
		case []any:
			for _, c := range x {
				rec(c, owner)
			}
		}
	}
	rec(v, "")
	return out
}

// recentChampions counts a player's champions over their last 20 games in the client's match history.
func (c *Client) recentChampions(puuid string, log RawLog) map[int]*champStat {
	if puuid == "" {
		return nil
	}
	v, err := c.getRaw("/lol-match-history/v1/products/lol/"+url.PathEscape(puuid)+"/matches?begIndex=0&endIndex=19", log)
	if err != nil {
		return nil
	}
	out := map[int]*champStat{}
	walk(v, func(o map[string]any) {
		parts, ok := o["participants"].([]any)
		if !ok || len(parts) == 0 {
			return
		}
		p, ok := parts[0].(map[string]any)
		if !ok {
			return
		}
		id, ok := firstNum(p, "championId")
		if !ok || id <= 0 {
			return
		}
		win := 0.0
		if st, ok := p["stats"].(map[string]any); ok {
			win, _ = firstNum(st, "win")
		}
		s := out[int(id)]
		if s == nil {
			s = &champStat{}
			out[int(id)] = s
		}
		s.games++
		s.wins += win
	})
	return out
}

var roleCode = map[string]string{"top": "T", "jungle": "J", "mid": "M", "adc": "B", "support": "S"}

// ClashFromClient builds a scoutable match from the client's Clash data: the opponent's players with
// their registered positions, and their champions from the scouting data (or their recent games).
// champs maps champion ids to names/aliases. It also returns each opponent's position by puuid.
func (c *Client) ClashFromClient(myPuuid string, champs map[int]Champion, log RawLog) (*ClashMatch, map[string]string, error) {
	r, err := c.ClashOpponent(myPuuid, log)
	if err != nil {
		return nil, nil, err
	}
	// players with a registered position first, so substitutes never push a starter out of the five shown
	sort.SliceStable(r.Members, func(a, b int) bool { return r.Members[a].Position != "" && r.Members[b].Position == "" })
	stats := c.scoutingChampions(r.Members, log)
	source := "scouting"
	if len(stats) == 0 {
		stats, source = map[string]map[int]*champStat{}, "recent games"
		for _, m := range r.Members {
			if s := c.recentChampions(m.Puuid, log); len(s) > 0 {
				stats[m.Puuid] = s
			}
		}
	}
	name := r.Name
	if name == "" {
		name = "Your opponent"
	}
	m := &ClashMatch{ID: "@client", Name: name + " · from the League client (" + source + ")"}
	pos := map[string]string{}
	for _, mem := range r.Members {
		p := ClashPlayer{ID: mem.Name, Role: roleCode[mem.Position]}
		if mem.Puuid != "" {
			pos[mem.Puuid] = mem.Position
		}
		st := stats[mem.Puuid]
		if st == nil {
			st = stats[mem.SummonerID]
		}
		for id, s := range st {
			ch, ok := champs[id]
			if !ok || s.games <= 0 {
				continue
			}
			p.Champs = append(p.Champs, ClashChamp{K: ch.Alias, G: s.games, WR: 100 * s.wins / s.games})
		}
		sort.SliceStable(p.Champs, func(a, b int) bool { return p.Champs[a].G > p.Champs[b].G })
		m.Them = append(m.Them, p)
	}
	return m, pos, nil
}

// CurrentPuuid is the signed-in player's puuid.
func (c *Client) CurrentPuuid() string {
	var s struct {
		Puuid string `json:"puuid"`
	}
	_ = c.Do("GET", "/lol-summoner/v1/current-summoner", nil, &s)
	return s.Puuid
}
