package core

import (
	"encoding/json"
	"testing"
)

func TestTopBans(t *testing.T) {
	var tl TierList
	_ = json.Unmarshal([]byte(`{"roles":{
		"top":[{"name":"Darius","id":"Darius","ban":0.18},{"name":"Garen","id":"Garen","ban":0.02},{"name":"Yone","id":"Yone","ban":0.21}],
		"mid":[{"name":"Yone","id":"Yone","ban":0.21},{"name":"Zed","id":"Zed","ban":0.30},{"name":"Ahri","id":"Ahri","ban":0}],
		"jungle":[{"name":"Wukong","id":"MonkeyKing","ban":0.18}]}}`), &tl)
	b := tl.TopBans(10)
	var names []string
	for _, x := range b {
		names = append(names, x.Name)
	}
	want := "Zed,Yone,Darius,Wukong,Garen" // by ban rate, each champion once, ties by name, no 0 % ones
	if got := join(names); got != want {
		t.Fatal(got)
	}
	if b[3].ID != "MonkeyKing" || len(tl.TopBans(2)) != 2 || (*TierList)(nil).TopBans(10) != nil {
		t.Fatal(b)
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

func TestSessionBans(t *testing.T) {
	s := Session{Actions: [][]Action{
		{{Type: "ban", ChampionID: 238, Completed: true}, {Type: "ban", ChampionID: 157, Completed: false, IsInProgress: true}},
		{{Type: "ban", ChampionID: 0, Completed: true}, {Type: "pick", ChampionID: 103, Completed: true}},
		{{Type: "ban", ChampionID: 122, Completed: true}},
	}}
	if got := s.Bans(); len(got) != 2 || got[0] != 238 || got[1] != 122 {
		t.Fatal("only finished bans count, not hovers, empty bans or picks", got)
	}
}
