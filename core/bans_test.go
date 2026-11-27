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

func TestEnemyIn(t *testing.T) {
	tl := &TierList{Roles: map[string][]TierEntry{
		"jungle": {{ID: "LeeSin", Name: "Lee Sin", Play: 900}, {ID: "Graves", Name: "Graves", Play: 500}},
		"mid":    {{ID: "Ahri", Name: "Ahri", Play: 950}, {ID: "LeeSin", Name: "Lee Sin", Play: 10}},
		"top":    {{ID: "Graves", Name: "Graves", Play: 500}},
	}}
	tl.index()
	en := []Champion{{Alias: "Ahri", Name: "Ahri"}, {Alias: "LeeSin", Name: "Lee Sin"}}
	if i, sure := EnemyIn("jungle", en, nil, tl); i != 1 || !sure {
		t.Fatal(i, sure)
	}
	if i, _ := EnemyIn("jungle", en, []string{"jungle", ""}, tl); i != 0 {
		t.Error("a known role wins", i)
	}
	if i, _ := EnemyIn("jungle", nil, nil, tl); i != -1 {
		t.Error("no enemies yet")
	}
}

func TestEnemyRoles(t *testing.T) {
	tl := &TierList{Roles: map[string][]TierEntry{
		"jungle": {{ID: "LeeSin", Name: "Lee Sin", Play: 900}},
		"mid":    {{ID: "Ahri", Name: "Ahri", Play: 950}},
	}}
	tl.index()
	roles, sure := EnemyRoles([]Champion{{Alias: "Ahri"}, {Alias: "LeeSin"}}, nil, tl)
	if roles[0] != "mid" || roles[1] != "jungle" || !sure[0] || !sure[1] {
		t.Fatal(roles, sure)
	}
}
