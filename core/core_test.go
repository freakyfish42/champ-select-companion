package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseLockfile(t *testing.T) {
	c, err := ParseLockfile("LeagueClient:15432:54321:Xy-z_9:https\n")
	if err != nil || c.PID != 15432 || c.Port != 54321 || c.Password != "Xy-z_9" {
		t.Fatalf("got %+v %v", c, err)
	}
	for _, bad := range []string{"", "LeagueClient:1:2", "LeagueClient:a:b:c:https", "LeagueClient:1:2::https"} {
		if _, err := ParseLockfile(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestMine(t *testing.T) {
	s := Session{LocalPlayerCellID: 2, MyTeam: []Member{{CellID: 1, ChampionID: 99}, {CellID: 2, PickIntent: 103, Position: "utility", Spell1: 4, Spell2: 14}}}
	p := s.Mine()
	if p.ChampionID != 103 || p.Locked || p.Role != "support" || p.Spell1 != 4 {
		t.Fatalf("intent: %+v", p)
	}
	s.Actions = [][]Action{{{ActorCellID: 1, ChampionID: 99, Completed: true, Type: "pick"}}, {{ActorCellID: 2, ChampionID: 266, Type: "ban", Completed: true}}, {{ActorCellID: 2, ChampionID: 84, IsInProgress: true, Type: "pick"}}}
	if p = s.Mine(); p.ChampionID != 84 || p.Locked {
		t.Fatalf("hover: %+v", p)
	}
	s.Actions[2][0].Completed, s.Actions[2][0].IsInProgress = true, false
	if p = s.Mine(); p.ChampionID != 84 || !p.Locked {
		t.Fatalf("locked: %+v", p)
	}
	for in, want := range map[string]string{"middle": "mid", "bottom": "adc", "utility": "support", "": "", "top": "top", "jungle": "jungle"} {
		if RoleFromPosition(in) != want {
			t.Errorf("%q", in)
		}
	}
	if !SupportedMode("CLASSIC") || SupportedMode("ARAM") || SupportedMode("CHERRY") {
		t.Error("modes")
	}
}

func TestSpellSlots(t *testing.T) {
	cases := [][6]int{
		{4, 14, 4, 14, 4, 14},   // same order
		{14, 4, 4, 14, 14, 4},   // Flash on F stays on F
		{4, 7, 14, 4, 4, 14},    // Flash on D stays on D
		{6, 7, 4, 14, 4, 14},    // nothing shared: op.gg's order
		{11, 4, 11, 14, 11, 14}, // Smite stays on D
	}
	for _, c := range cases {
		if a, b := SpellSlots(c[0], c[1], c[2], c[3]); a != c[4] || b != c[5] {
			t.Errorf("%v → %d,%d", c, a, b)
		}
	}
}

func TestPageNameAndPerks(t *testing.T) {
	if n := PageName("Ahri", "mid"); n != "Ahri Mid" {
		t.Fatal(n)
	}
	if n := PageName("Nunu & Willump", "jungle"); n != "Nunu & Willump Jungle" {
		t.Fatal(n)
	}
	if n := PageName("Ahri", ""); n != "Ahri" {
		t.Fatal(n)
	}
	b := sampleBuild(t)
	ids, err := PerkIDs(b.Runes[0])
	if err != nil || len(ids) != 9 || ids[0] != 8112 || ids[4] != 8226 || ids[8] != 5011 {
		t.Fatal(ids, err)
	}
	bad := b.Runes[0]
	bad.Shards = bad.Shards[:2]
	if _, err := PerkIDs(bad); err == nil {
		t.Fatal("incomplete page accepted")
	}
}

func TestOrderAndPrefs(t *testing.T) {
	b := sampleBuild(t)
	r, s := Order(b, Pref{})
	if r[0] != 0 || s[0] != 0 {
		t.Fatal(r, s)
	}
	r, s = Order(b, Pref{Keystone: 8229, Spells: []int{12, 4}})
	if r[0] != 1 || r[1] != 0 || s[0] != 1 {
		t.Fatal(r, s)
	}
	// two pages with the same keystone: the secondary tree decides
	b2 := sampleBuild(t)
	b2.Runes[1].Keys[0] = b2.Runes[0].Keys[0]
	if r, _ = Order(b2, Pref{Keystone: 8112, Sub: 8300}); r[0] != 1 {
		t.Fatal(r)
	}
	if r, _ = Order(b2, Pref{Keystone: 8112, Sub: 8200}); r[0] != 0 {
		t.Fatal(r)
	}
	path := filepath.Join(t.TempDir(), "p", "prefs.json")
	p := LoadPrefs(path)
	p.Set("Ahri:mid", func(x *Pref) { x.Keystone = 8229 })
	p.Set("Ahri:mid", func(x *Pref) { x.Spells = []int{4, 12} })
	q := LoadPrefs(path)
	if g := q.Get("Ahri:mid"); g.Keystone != 8229 || len(g.Spells) != 2 {
		t.Fatal(g)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		w    bool
	}{{"v1.0.0", "v1.0.1", true}, {"v1.0.9", "v1.1.0", true}, {"v1.2.0", "v1.2.0", false}, {"v2.0.0", "v1.9.9", false}, {"dev", "v9.0.0", false}, {"v1.0.0", "garbage", false}, {"v1.0.0", "v1.0.1-beta", true}} {
		if Newer(c.a, c.b) != c.w {
			t.Errorf("%v", c)
		}
	}
}

// ---- a fake League client ----

type fakeLCU struct {
	mu       sync.Mutex
	pages    []map[string]any
	owned    int
	nextID   int
	current  int
	spells   map[string]int
	calls    []string
	password string
}

func newFake(owned int, pages ...map[string]any) (*fakeLCU, *httptest.Server) {
	f := &fakeLCU{pages: pages, owned: owned, nextID: 900, password: "pw"}
	srv := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	return f, srv
}

func (f *fakeLCU) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != "riot" || p != f.password {
		w.WriteHeader(401)
		return
	}
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	body, _ := io.ReadAll(r.Body)
	out := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == "GET" && r.URL.Path == "/lol-perks/v1/pages":
		out(f.pages)
	case r.Method == "GET" && r.URL.Path == "/lol-perks/v1/inventory":
		out(map[string]int{"ownedPageCount": f.owned})
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/lol-perks/v1/pages/"):
		id := strings.TrimPrefix(r.URL.Path, "/lol-perks/v1/pages/")
		for i, p := range f.pages {
			if jsonNum(p["id"]) == id {
				f.pages = append(f.pages[:i], f.pages[i+1:]...)
				w.WriteHeader(204)
				return
			}
		}
		w.WriteHeader(404)
	case r.Method == "POST" && r.URL.Path == "/lol-perks/v1/pages":
		var p map[string]any
		_ = json.Unmarshal(body, &p)
		if ids, _ := p["selectedPerkIds"].([]any); len(ids) != 9 {
			w.WriteHeader(400)
			out(map[string]string{"message": "bad perks"})
			return
		}
		deletable := 0
		for _, x := range f.pages {
			if x["isDeletable"] == true {
				deletable++
			}
		}
		if deletable >= f.owned {
			w.WriteHeader(400)
			out(map[string]string{"message": "Max pages reached"})
			return
		}
		f.nextID++
		p["id"], p["isDeletable"], p["isEditable"] = f.nextID, true, true
		f.pages = append(f.pages, p)
		f.current = f.nextID
		out(p)
	case r.Method == "PUT" && r.URL.Path == "/lol-perks/v1/currentpage":
		_ = json.Unmarshal(body, &f.current)
		w.WriteHeader(204)
	case r.Method == "PATCH" && r.URL.Path == "/lol-champ-select/v1/session/my-selection":
		_ = json.Unmarshal(body, &f.spells)
		w.WriteHeader(204)
	case r.Method == "GET" && r.URL.Path == "/lol-gameflow/v1/gameflow-phase":
		out("ChampSelect")
	case r.Method == "GET" && r.URL.Path == "/lol-gameflow/v1/session":
		out(map[string]any{"map": map[string]any{"gameMode": "CLASSIC"}, "gameData": map[string]any{"queue": map[string]any{"gameMode": "CLASSIC", "id": 700, "type": "CLASH"}}})
	case r.Method == "GET" && r.URL.Path == "/lol-game-data/assets/v1/champion-summary.json":
		out([]map[string]any{{"id": -1, "name": "None", "alias": "None"}, {"id": 103, "name": "Ahri", "alias": "Ahri"}, {"id": 62, "name": "Wukong", "alias": "MonkeyKing"}})
	default:
		w.WriteHeader(404)
	}
}

func jsonNum(v any) string { b, _ := json.Marshal(v); return string(b) }

func client(srv *httptest.Server, pw string) *Client {
	return NewClientURL(srv.URL, pw, srv.Client())
}

func page(id int, name string, deletable bool) map[string]any {
	return map[string]any{"id": id, "name": name, "isDeletable": deletable, "isEditable": deletable}
}

func TestApplyRunes(t *testing.T) {
	b := sampleBuild(t)
	name := PageName("Ahri", "mid")

	// a free slot: a new page is made and becomes current, nothing deleted
	f, srv := newFake(3, page(1, "Preset 1", false), page(10, "My Ahri", true))
	c := client(srv, "pw")
	r := c.ApplyRunes(b.Runes[0], name, nil, 0, false)
	if !r.OK || r.PageID != 901 {
		t.Fatal(r)
	}
	if len(f.pages) != 3 || f.pages[2]["name"] != "Ahri Mid" || f.current != 901 {
		t.Fatalf("%+v cur=%d", f.pages, f.current)
	}
	own := []int{r.PageID}
	// second click: the app's own page (known by id) is replaced, the player's page untouched
	if r := c.ApplyRunes(b.Runes[1], name, own, 0, false); !r.OK {
		t.Fatal(r)
	}
	if len(f.pages) != 3 || f.pages[1]["name"] != "My Ahri" || jsonNum(f.pages[2]["primaryStyleId"]) != "8200" {
		t.Fatalf("%+v", f.pages)
	}
	// a player's page that happens to share the name is not the app's
	f2, srv2 := newFake(3, page(10, "Ahri Mid", true))
	if r := client(srv2, "pw").ApplyRunes(b.Runes[0], name, own, 0, false); !r.OK || len(f2.pages) != 2 || jsonNum(f2.pages[0]["id"]) != "10" {
		t.Fatalf("%+v %+v", r, f2.pages)
	}
	srv2.Close()
	srv.Close()

	// a page from an early test build ("lømsland · ...") still counts as the app's and is renamed
	f, srv = newFake(2, page(10, "A", true), page(11, "lømsland · Ahri Mid", true))
	if r := client(srv, "pw").ApplyRunes(b.Runes[0], name, nil, 0, false); !r.OK || len(f.pages) != 2 || f.pages[1]["name"] != "Ahri Mid" {
		t.Fatalf("%+v %+v", r, f.pages)
	}
	srv.Close()

	// all slots used: nothing changes, the player's pages are offered
	f, srv = newFake(2, page(1, "Preset", false), page(10, "A", true), page(11, "B", true))
	c = client(srv, "pw")
	r = c.ApplyRunes(b.Runes[0], name, own, 0, false)
	if r.OK || len(r.Replace) != 2 || r.Replace[0].ID != 10 {
		t.Fatalf("%+v", r)
	}
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "GET") {
			t.Fatalf("changed something: %v", f.calls)
		}
	}
	// test mode describes and changes nothing
	if r := c.ApplyRunes(b.Runes[0], name, own, 11, true); !r.OK || !strings.Contains(r.Msg, "“B”") || len(f.pages) != 3 {
		t.Fatalf("%+v", r)
	}
	// the player picks B to replace
	if r := c.ApplyRunes(b.Runes[0], name, own, 11, false); !r.OK {
		t.Fatal(r)
	}
	if len(f.pages) != 3 || f.pages[1]["name"] != "A" || f.pages[2]["name"] != name {
		t.Fatalf("%+v", f.pages)
	}
	srv.Close()

	// a page id that isn't one of the player's own pages is refused, and Riot's preset stays
	f, srv = newFake(1, page(1, "Preset", false), page(10, "A", true))
	if r := client(srv, "pw").ApplyRunes(b.Runes[0], name, nil, 1, false); r.OK || len(f.pages) != 2 {
		t.Fatalf("%+v %+v", r, f.pages)
	}
	srv.Close()

	// wrong password: a readable error
	_, srv = newFake(3)
	if r := client(srv, "nope").ApplyRunes(b.Runes[0], name, nil, 0, false); r.OK || !strings.Contains(r.Msg, "401") {
		t.Fatalf("%+v", r)
	}
	srv.Close()
}

func TestApplySpellsAndReads(t *testing.T) {
	f, srv := newFake(2)
	defer srv.Close()
	c := client(srv, "pw")
	if r := c.ApplySpells([]int{4, 14}, nil, 14, 4, false); !r.OK || f.spells["spell1Id"] != 14 || f.spells["spell2Id"] != 4 {
		t.Fatalf("%+v %v", r, f.spells)
	}
	if r := c.ApplySpells([]int{4}, nil, 0, 0, false); r.OK {
		t.Fatal("one spell accepted")
	}
	if r := c.ApplySpells([]int{4, 12}, map[int]string{4: "Flash", 12: "Teleport"}, 12, 4, true); !r.OK || r.Msg != "Test mode: would set Teleport on D and Flash on F." {
		t.Fatal(r)
	}
	if ph, err := c.Phase(); err != nil || ph != "ChampSelect" {
		t.Fatal(ph, err)
	}
	if g, err := c.Game(); err != nil || g.Mode != "CLASSIC" || !g.Clash {
		t.Fatal(g, err)
	}
	ch, err := c.Champions()
	if err != nil || ch[62].Alias != "MonkeyKing" || len(ch) != 2 {
		t.Fatal(ch, err)
	}
}

func TestFetchBuild(t *testing.T) {
	raw := sampleJSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("c") == "Nobody" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"No champion called “Nobody”."}`))
			return
		}
		if r.URL.Query().Get("c") != "Ahri" || r.URL.Query().Get("p") != "mid" {
			t.Errorf("query %v", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()
	b, err := FetchBuild(srv.Client(), srv.URL, "Ahri", "mid")
	if err != nil || b.Champion != "Ahri" || len(b.Runes) != 2 || b.Runes[0].Keystone() != 8112 || len(b.Raw) == 0 {
		t.Fatal(b, err)
	}
	if _, err := FetchBuild(srv.Client(), srv.URL, "Nobody", ""); err == nil || !strings.Contains(err.Error(), "Nobody") {
		t.Fatal(err)
	}
}

// shaped like the site's /api/build answer
const sampleJSON = `{"champion":"Ahri","id":"Ahri","position":"mid","region":"EUW",
"runes":[{"primary":{"id":8100,"name":"Domination","icon":"a.png"},"secondary":{"id":8200,"name":"Sorcery"},"keys":[{"id":8112,"name":"Electrocute"},{"id":8139,"name":"Taste of Blood"},{"id":8138,"name":"Eyeball Collection"},{"id":8135,"name":"Treasure Hunter"}],"subs":[{"id":8226,"name":"Manaflow Band"},{"id":8210,"name":"Transcendence"}],"shards":[{"id":5008,"name":"Adaptive Force"},{"id":5008,"name":"Adaptive Force"},{"id":5011,"name":"Health"}],"pick":0.62,"wr":0.521,"play":50000},
{"primary":{"id":8200,"name":"Sorcery"},"secondary":{"id":8300,"name":"Inspiration"},"keys":[{"id":8229,"name":"Arcane Comet"},{"id":8226,"name":"Manaflow Band"},{"id":8210,"name":"Transcendence"},{"id":8237,"name":"Scorch"}],"subs":[{"id":8345,"name":"Biscuit Delivery"},{"id":8347,"name":"Cosmic Insight"}],"shards":[{"id":5008,"name":"Adaptive Force"},{"id":5008,"name":"Adaptive Force"},{"id":5011,"name":"Health"}],"pick":0.21,"wr":0.504,"play":17000}],
"summs":[{"ids":[4,14],"spells":[{"name":"Flash","img":"SummonerFlash.png"},{"name":"Ignite","img":"SummonerDot.png"}],"pick":0.7,"wr":0.51,"play":90000},{"ids":[4,12],"pick":0.24,"wr":0.5,"play":30000}]}`

func sampleBuild(t *testing.T) *Build {
	var b Build
	if err := json.Unmarshal([]byte(sampleJSON), &b); err != nil {
		t.Fatal(err)
	}
	return &b
}

// a real build.lømsland.no answer (Ahri mid, 2026-11-25) gives valid pages for the client
func TestRealSiteAnswer(t *testing.T) {
	raw, err := os.ReadFile("testdata/ahri_mid.json")
	if err != nil {
		t.Fatal(err)
	}
	var b Build
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if b.Position != "mid" || len(b.Runes) != 2 || len(b.Summs) != 2 || b.Summs[1].Spells[1].Name != "Teleport" {
		t.Fatalf("%+v", b)
	}
	for i, p := range b.Runes {
		ids, err := PerkIDs(p)
		if err != nil || len(ids) != 9 {
			t.Fatalf("page %d: %v %v", i, ids, err)
		}
	}
	ids, _ := PerkIDs(b.Runes[0])
	if want := []int{8112, 8139, 8140, 8106, 8210, 8226, 5005, 5008, 5001}; jsonNum(ids) != jsonNum(want) {
		t.Fatal(ids)
	}
}

func TestSignedUpdates(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	exe := []byte("MZ....junk " + VersionMarker("dev") + " more " + VersionMarker("v1.2.3") + " end")
	if TagInBinary(exe) != "v1.2.3" {
		t.Fatal(TagInBinary(exe))
	}
	tag, sig, err := SignRelease(priv, exe)
	if err != nil || tag != "v1.2.3" {
		t.Fatal(tag, err)
	}
	if err := VerifyRelease(pubB64, "v1.2.3", exe, sig); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, exe...)
	bad[3] = 'X'
	if VerifyRelease(pubB64, "v1.2.3", bad, sig) == nil {
		t.Fatal("changed exe accepted")
	}
	if VerifyRelease(pubB64, "v1.2.4", exe, sig) == nil {
		t.Fatal("an old release passed off as a new tag")
	}
	pub2, _, _ := ed25519.GenerateKey(nil)
	if VerifyRelease(base64.StdEncoding.EncodeToString(pub2), "v1.2.3", exe, sig) == nil {
		t.Fatal("other key accepted")
	}
	if VerifyRelease("", "v1.2.3", exe, sig) == nil || VerifyRelease(pubB64, "v1.2.3", exe, []byte("nope")) == nil {
		t.Fatal("missing key or junk signature accepted")
	}
	if _, _, err := SignRelease(priv, []byte("no marker "+VersionMarker("dev"))); err == nil {
		t.Fatal("signed a file with no version")
	}
}

func TestRiotCertificate(t *testing.T) {
	c, err := riotRootCert()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(c.Raw)
	if hex.EncodeToString(sum[:]) != "ca8c9d325b4cdc464c6c94a585c85e91ec23d40ba5bf3ae2822b951a4a504ea3" || c.Subject.CommonName != "LoL Game Engineering Certificate Authority" {
		t.Fatalf("%x %s", sum, c.Subject)
	}
	// a server with any other certificate is refused
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`"ChampSelect"`)) }))
	defer srv.Close()
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: RiotTLS()}}
	if _, err := NewClientURL(srv.URL, "pw", hc).Phase(); err == nil || !strings.Contains(err.Error(), "certificate isn't Riot's") {
		t.Fatal(err)
	}
}

func TestSignedBy(t *testing.T) {
	load := func(n string) *x509.Certificate {
		b, err := os.ReadFile("testdata/" + n)
		if err != nil {
			t.Fatal(err)
		}
		p, _ := pem.Decode(b)
		c, err := x509.ParseCertificate(p.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ca := load("ca.pem")
	for _, n := range []string{"leaf_sha1.pem", "leaf_sha256.pem"} {
		if err := signedBy([]*x509.Certificate{load(n)}, ca); err != nil {
			t.Errorf("%s: %v", n, err)
		}
		if err := signedBy([]*x509.Certificate{load(n), ca}, ca); err != nil {
			t.Errorf("%s with chain: %v", n, err)
		}
	}
	if signedBy([]*x509.Certificate{load("other.pem")}, ca) == nil {
		t.Error("a certificate from another root was accepted")
	}
	riot, _ := riotRootCert()
	if signedBy([]*x509.Certificate{load("leaf_sha1.pem")}, riot) == nil {
		t.Error("a test certificate passed as Riot's")
	}
}

func TestQueueName(t *testing.T) {
	cases := []struct {
		id    int
		desc  string
		mode  string
		clash bool
		want  string
	}{
		{420, "5v5 Ranked Solo games", "CLASSIC", false, "Ranked Solo/Duo"},
		{440, "", "CLASSIC", false, "Ranked Flex"},
		{400, "", "CLASSIC", false, "Normal Draft"},
		{700, "", "CLASSIC", true, "Clash"},
		{0, "", "CLASSIC", true, "Clash"}, // the client says Clash: trust it
		{450, "", "ARAM", false, "ARAM"},
		{-1, "", "CLASSIC", false, "Custom game"},
		{9999, "Some new event queue with a very long name", "CLASSIC", false, "Some new event queue with a …"},
		{9999, "", "", false, ""},
	}
	for _, c := range cases {
		if got := QueueName(c.id, c.desc, c.mode, c.clash); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
