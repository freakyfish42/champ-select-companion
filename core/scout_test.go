package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a small tier list: play counts per role
func tier() *TierList {
	t := &TierList{Roles: map[string][]TierEntry{
		"top":     {{"Darius", "Darius", 9000, 0}, {"K'Sante", "KSante", 5000, 0}, {"Yasuo", "Yasuo", 2000, 0}, {"Jax", "Jax", 4000, 0}},
		"jungle":  {{"Lee Sin", "LeeSin", 9000, 0}, {"Kayn", "Kayn", 6000, 0}, {"Graves", "Graves", 4000, 0}, {"Jax", "Jax", 1500, 0}},
		"mid":     {{"Ahri", "Ahri", 9000, 0}, {"Yasuo", "Yasuo", 6000, 0}, {"Syndra", "Syndra", 5000, 0}, {"Yone", "Yone", 5000, 0}},
		"adc":     {{"Jinx", "Jinx", 9000, 0}, {"Kai'Sa", "Kaisa", 8000, 0}, {"Ezreal", "Ezreal", 7000, 0}, {"Varus", "Varus", 3000, 0}},
		"support": {{"Thresh", "Thresh", 9000, 0}, {"Rakan", "Rakan", 4000, 0}, {"Blitzcrank", "Blitzcrank", 3000, 0}},
	}}
	t.index()
	return t
}

func TestAssignRoles(t *testing.T) {
	tl := tier()
	w := func(champs ...string) [][5]float64 {
		out := make([][5]float64, len(champs))
		for i, c := range champs {
			for r, role := range Roles {
				out[i][r] = tl.Share(c, role)
			}
		}
		return out
	}
	// Yasuo is mostly mid, but Ahri is surely mid, so Yasuo goes top
	if got := AssignRoles(w("Yasuo", "Ahri", "Thresh"), nil); strings.Join(got, ",") != "top,mid,support" {
		t.Fatal(got)
	}
	// a known role is kept
	if got := AssignRoles(w("Jax", "Darius"), []string{"jungle", ""}); strings.Join(got, ",") != "jungle,top" {
		t.Fatal(got)
	}
	if got := AssignRoles(nil, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if tl.Share("kai'sa", "adc") != 1 || tl.Share("KAISA", "adc") != 1 || tl.Share("nobody", "mid") != 0 {
		t.Fatal("share lookup")
	}
}

func TestLaneMatchup(t *testing.T) {
	tl := tier()
	b := &Build{Position: "mid", Vs: []VsEntry{{ID: "Yasuo", Name: "Yasuo", WR: 0.47, Play: 900}, {ID: "Syndra", Name: "Syndra", WR: 0.55, Play: 1600}}}
	en := func(n ...string) []Champion {
		var out []Champion
		for _, x := range n {
			out = append(out, Champion{Name: x, Alias: strings.ReplaceAll(x, " ", "")})
		}
		return out
	}
	m := LaneMatchup(b, "mid", en("Thresh", "Syndra"), nil, tl)
	if m.State != "ok" || m.Opp != "Syndra" || *m.WR != 0.55 || m.Play != 1600 || !m.Sure {
		t.Fatalf("%+v", m)
	}
	// Yasuo with a sure mid laner on their team plays top: no mid yet
	if m := LaneMatchup(b, "mid", en("Yasuo", "Ahri"), nil, tl); m.Opp != "Ahri" || m.State != "nodata" {
		t.Fatalf("%+v", m)
	}
	if m := LaneMatchup(b, "mid", en("Thresh", "Jinx"), nil, tl); m.State != "waiting" || m.Opp != "" {
		t.Fatalf("%+v", m)
	}
	if m := LaneMatchup(b, "", en("Syndra"), nil, tl); m.State != "waiting" {
		t.Fatalf("%+v", m)
	}
	m = LaneMatchup(b, "mid", en("Yasuo"), nil, tl)
	if m.Opp != "Yasuo" || !m.Sure || *m.WR != 0.47 {
		t.Fatalf("Yasuo (75%% mid) alone: %+v", m)
	}
	// Clash: the client says who plays where, so Yasuo registered as mid is sure, even next to Ahri
	if m := LaneMatchup(b, "mid", en("Yasuo", "Ahri"), []string{"mid", ""}, tl); m.Opp != "Yasuo" || !m.Sure || m.State != "ok" {
		t.Fatalf("%+v", m)
	}
	// with Darius in top, Jax is their jungler, but only a guess
	if m := LaneMatchup(&Build{}, "jungle", en("Jax", "Darius"), nil, tl); m.Opp != "Jax" || m.Sure || m.State != "nodata" {
		t.Fatalf("%+v", m)
	}
}

func TestScoutTeam(t *testing.T) {
	tl := tier()
	m := &ClashMatch{ID: "abc", Name: "Round 1", Them: []ClashPlayer{
		{ID: "Enemy One#EUW", Champs: []ClashChamp{{K: "thresh", G: 30, WR: 52}, {K: "rakan", G: 20, WR: 55}, {K: "blitzcrank", G: 8, WR: 50}}},
		{ID: "Enemy Two#EUW", Champs: []ClashChamp{{K: "yasuo", G: 60, WR: 51}, {K: "yone", G: 25, WR: 56}, {K: "syndra", G: 10, WR: 40}}},
		{ID: "Enemy Three#EUW", Champs: []ClashChamp{{K: "kaisa", G: 45, WR: 54}, {K: "ezreal", G: 20, WR: 50}, {K: "varus", G: 12, WR: 58}}},
		{ID: "Enemy Four#EUW", Champs: []ClashChamp{{K: "kayn", G: 33, WR: 57}, {K: "graves", G: 15, WR: 47}}},
		{ID: "Enemy Five#EUW", Role: "T", Champs: []ClashChamp{{K: "ksante", G: 28, WR: 50}, {K: "jax", G: 22, WR: 59}, {K: "darius", G: 0, WR: 0, RG: 6, Season: true}}},
		{ID: "", Champs: nil},
	}}
	s := ScoutTeam(m, tl)
	got := []string{}
	for _, p := range s.Players {
		got = append(got, p.Role+":"+p.Name)
	}
	if strings.Join(got, ",") != "top:Enemy Five#EUW,jungle:Enemy Four#EUW,mid:Enemy Two#EUW,adc:Enemy Three#EUW,support:Enemy One#EUW" {
		t.Fatal(got)
	}
	if s.Players[0].Guessed || !s.Players[1].Guessed {
		t.Fatal("saved role is not a guess")
	}
	top := s.Players[0].Champs
	if len(top) != 3 || top[0].Name != "K'Sante" || top[0].ID != "KSante" || top[2].Name != "Darius" || top[2].Games != 6 {
		t.Fatalf("%+v", top)
	}
	if s.Players[1].Champs[0].WR != 0.57 {
		t.Fatalf("%+v", s.Players[1].Champs)
	}
	if len(s.Bans) != 3 || s.Bans[0].Name != "Yasuo" || s.Bans[0].Player != "Enemy Two#EUW" {
		t.Fatalf("%+v", s.Bans)
	}
}

func TestClashStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		if r.Method != "POST" || r.URL.Path != "/api/store" || r.Header.Get("x-pass") != "" {
			w.WriteHeader(400)
			return
		}
		switch b["op"] {
		case "list":
			_, _ = w.Write([]byte(`{"matches":[{"id":"old","name":"Old","updated":1},{"id":"new","name":"Final","updated":9}]}`))
		case "get":
			if b["id"] == "gone" {
				_, _ = w.Write([]byte(`{"match":null}`))
				return
			}
			_, _ = w.Write([]byte(`{"match":{"id":"new","name":"Final","updated":9,"us":[],"them":[{"id":"A#EUW","role":"M","champs":[{"k":"ahri","g":10,"wr":60}]}]}}`))
		}
	}))
	defer srv.Close()
	list, err := ClashMatches(srv.Client(), srv.URL)
	if err != nil || len(list) != 2 || list[0].ID != "new" {
		t.Fatal(list, err)
	}
	m, err := GetClashMatch(srv.Client(), srv.URL, "new")
	if err != nil || m.Name != "Final" || m.Them[0].Champs[0].K != "ahri" {
		t.Fatal(m, err)
	}
	if _, err := GetClashMatch(srv.Client(), srv.URL, "gone"); err == nil {
		t.Fatal("missing match accepted")
	}
}

func TestCounters(t *testing.T) {
	syndra := &Build{Vs: []VsEntry{{"Ahri", "Ahri", 0.4483, 1682}, {"Zed", "Zed", 0.52, 900}, {"Kassadin", "Kassadin", 0.45, 300}, {"Vex", "Vex", 0.49, 120}, {"Lux", "Lux", 0.43, 80}}}
	c := Counters(syndra, 3)
	if len(c) != 3 || c[0].Name != "Lux" || c[0].WR != 0.57 || c[1].Name != "Ahri" || c[1].WR != 0.5517 || c[2].Name != "Kassadin" {
		t.Fatalf("%+v", c)
	}
	if len(Counters(&Build{Vs: []VsEntry{{"Zed", "Zed", 0.55, 900}}}, 3)) != 0 {
		t.Fatal("a champion that loses to them is no counter")
	}
	if Counters(nil, 3) != nil {
		t.Fatal("nil")
	}
}
