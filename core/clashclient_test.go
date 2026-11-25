package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a fake client in Clash: our team r1 (round 1 won), now facing r2
func fakeClash(t *testing.T, scouting bool) (*httptest.Server, *[]string) {
	logs := &[]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := func(s string) { w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(s)) }
		switch r.URL.Path {
		case "/lol-clash/v1/player":
			out(`{"tournamentId":7,"rosterId":"r1","primaryPos":"MIDDLE"}`)
		case "/lol-clash/v1/thirdparty/team-data":
			out(`{"rosterId":"r1"}`)
		case "/lol-clash/v1/tournament-summary":
			w.WriteHeader(404)
		case "/lol-clash/v1/roster/r1":
			out(`{"id":"r1","name":"Our Team","bracketId":55,"members":[{"puuid":"me","position":"MIDDLE","gameName":"Sakura Kyoko","tagLine":"EUW"}]}`)
		case "/lol-clash/v1/bracket/55":
			out(`{"id":55,"matches":[{"roundId":1,"rosterId1":"r1","rosterId2":"r9","winnerId":"r1"},{"roundId":2,"rosterId1":"r3","rosterId2":"r1","winnerId":0},{"roundId":2,"rosterId1":"r4","rosterId2":"r5"}]}`)
		case "/lol-clash/v1/roster/r3":
			out(`{"id":"r3","name":"Rivals","members":[
			 {"puuid":"e1","summonerId":11,"position":"MIDDLE","gameName":"Rival Mid","tagLine":"EUW"},
			 {"puuid":"e2","summonerId":12,"position":"UTILITY","summonerName":"Rival Supp"},
			 {"puuid":"e3","summonerId":13,"position":"FILL","gameName":"Rival Fill","tagLine":"1234"}]}`)
		case "/lol-clash/v1/scouting/champions":
			if !scouting {
				w.WriteHeader(404)
				return
			}
			out(`[{"puuid":"e1","championScoutingData":[{"championId":157,"gameCount":60,"winCount":31},{"championId":103,"gameCount":"10","winRate":0.4}]},
			      {"summonerId":12,"champions":[{"championId":412,"games":30,"wins":18}]}]`)
		case "/lol-match-history/v1/products/lol/e1/matches":
			out(`{"games":{"games":[{"participants":[{"championId":157,"stats":{"win":true}}]},{"participants":[{"championId":157,"stats":{"win":false}}]},{"participants":[{"championId":238,"stats":{"win":true}}]}]}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	return srv, logs
}

func clashChamps() map[int]Champion {
	return map[int]Champion{157: {157, "Yasuo", "Yasuo"}, 103: {103, "Ahri", "Ahri"}, 412: {412, "Thresh", "Thresh"}, 238: {238, "Zed", "Zed"}}
}

func TestClashFromClientScouting(t *testing.T) {
	srv, _ := fakeClash(t, true)
	defer srv.Close()
	c := NewClientURL(srv.URL, "pw", srv.Client())
	var seen []string
	m, pos, err := c.ClashFromClient("me", clashChamps(), func(p string, b []byte, err error) { seen = append(seen, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.Name, "Rivals · from the League client (scouting)") || len(m.Them) != 3 {
		t.Fatalf("%+v", m)
	}
	mid := m.Them[0]
	if mid.ID != "Rival Mid#EUW" || mid.Role != "M" || len(mid.Champs) != 2 || mid.Champs[0].K != "Yasuo" || mid.Champs[0].G != 60 {
		t.Fatalf("%+v", mid)
	}
	if mid.Champs[1].K != "Ahri" || mid.Champs[1].WR != 40 {
		t.Fatalf("win rate from a rate field: %+v", mid.Champs[1])
	}
	if m.Them[1].ID != "Rival Supp" || m.Them[1].Role != "S" || m.Them[1].Champs[0].K != "Thresh" || m.Them[1].Champs[0].WR != 60 {
		t.Fatalf("player found by summoner id: %+v", m.Them[1])
	}
	if m.Them[2].Role != "" || m.Them[2].ID != "Rival Fill#1234" {
		t.Fatalf("fill has no role: %+v", m.Them[2])
	}
	if pos["e1"] != "mid" || pos["e2"] != "support" || pos["e3"] != "" {
		t.Fatal(pos)
	}
	if !strings.Contains(strings.Join(seen, " "), "/lol-clash/v1/bracket/55") {
		t.Fatal("raw answers are logged", seen)
	}
	// scouted: the client's roles are not guesses, and Yasuo is a ban idea
	s := ScoutTeam(m, tier())
	if s.Players[0].Role != "mid" || s.Players[0].Guessed || s.Players[2].Role != "" || s.Bans[0].Name != "Yasuo" {
		t.Fatalf("%+v", s)
	}
}

func TestClashFromClientRecentGames(t *testing.T) {
	srv, _ := fakeClash(t, false)
	defer srv.Close()
	c := NewClientURL(srv.URL, "pw", srv.Client())
	m, _, err := c.ClashFromClient("me", clashChamps(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Name, "(recent games)") || len(m.Them[0].Champs) != 2 || m.Them[0].Champs[0].K != "Yasuo" || m.Them[0].Champs[0].G != 2 || m.Them[0].Champs[0].WR != 50 {
		t.Fatalf("%+v", m.Them[0])
	}
}

func TestClashOpponentErrors(t *testing.T) {
	// not in Clash: the client knows no roster
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv.Close()
	if _, err := NewClientURL(srv.URL, "pw", srv.Client()).ClashOpponent("me", nil); err == nil || !strings.Contains(err.Error(), "didn't show your Clash team") {
		t.Fatal(err)
	}
	// round 1 already decided and no next match yet
	srv2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lol-clash/v1/player":
			_, _ = w.Write([]byte(`{"rosterId":"r1"}`))
		case "/lol-clash/v1/roster/r1":
			_, _ = w.Write([]byte(`{"id":"r1","bracketId":"b","members":[{"puuid":"me"}]}`))
		case "/lol-clash/v1/bracket/b":
			_, _ = w.Write([]byte(`{"matches":[{"roundId":1,"rosterId1":"r1","rosterId2":"r2","winnerId":"r1"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv2.Close()
	if _, err := NewClientURL(srv2.URL, "pw", srv2.Client()).ClashOpponent("me", nil); err == nil || !strings.Contains(err.Error(), "no opponent") {
		t.Fatal(err)
	}
}

func TestParseScoutChampsShapes(t *testing.T) {
	ms := []ClashMember{{Puuid: "p1"}}
	var v any
	// keyed by the player's id
	_ = json.Unmarshal([]byte(`{"p1":[{"championId":1,"count":5,"wins":3}]}`), &v)
	if s := parseScoutChamps(v, ms); s["p1"][1].games != 5 || s["p1"][1].wins != 3 {
		t.Fatalf("%+v", s)
	}
	// strangers are ignored
	_ = json.Unmarshal([]byte(`[{"puuid":"x","champions":[{"championId":1,"games":5}]}]`), &v)
	if s := parseScoutChamps(v, ms); len(s) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestEnemyPicksPuuid(t *testing.T) {
	s := Session{TheirTeam: []Member{{ChampionID: 157, Puuid: "e1"}, {ChampionID: 0}, {ChampionID: 412}}}
	p := s.EnemyPicks()
	if len(p) != 2 || p[0].Puuid != "e1" || p[1].ChampionID != 412 {
		t.Fatal(p)
	}
}

// a substitute listed first must not push a starter out of the five shown
func TestClashSubstituteDoesNotHideAStarter(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lol-clash/v1/player":
			_, _ = w.Write([]byte(`{"rosterId":"r1"}`))
		case "/lol-clash/v1/roster/r1":
			_, _ = w.Write([]byte(`{"id":"r1","bracketId":"b","members":[{"puuid":"me"}]}`))
		case "/lol-clash/v1/bracket/b":
			_, _ = w.Write([]byte(`{"matches":[{"roundId":1,"rosterId1":"r1","rosterId2":"r2"}]}`))
		case "/lol-clash/v1/roster/r2":
			_, _ = w.Write([]byte(`{"members":[{"puuid":"sub","gameName":"Sub"},{"puuid":"a","position":"TOP","gameName":"A"},{"puuid":"b","position":"JUNGLE","gameName":"B"},{"puuid":"c","position":"MIDDLE","gameName":"C"},{"puuid":"d","position":"BOTTOM","gameName":"D"},{"puuid":"e","position":"UTILITY","gameName":"E"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	m, _, err := NewClientURL(srv.URL, "pw", srv.Client()).ClashFromClient("me", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := ScoutTeam(m, tier())
	names := []string{}
	for _, p := range s.Players {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "A,B,C,D,E" {
		t.Fatal(names)
	}
}
