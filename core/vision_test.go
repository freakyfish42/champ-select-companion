package core

// Vision tests: the owner's original requirements for the app, as tests. If one of these fails, a change
// has broken what the app is meant to be. Each test names the requirement it guards (see VISION.md).
// build.sh runs them before every build.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a fake League client that answers everything it knows and remembers every request.
type recorder struct {
	mu    sync.Mutex
	calls []string // "METHOD path body"
	full  bool
	page  string // the last rune page written, served back as the active page
}

func (rc *recorder) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	rc.mu.Lock()
	rc.calls = append(rc.calls, r.Method+" "+r.URL.Path+" "+string(b))
	full := rc.full
	if (r.Method == "POST" || r.Method == "PUT") && strings.HasPrefix(r.URL.Path, "/lol-perks/v1/pages") {
		var pg map[string]any
		_ = json.Unmarshal(b, &pg)
		if pg["id"] == nil {
			pg["id"] = 77
		}
		j, _ := json.Marshal(pg)
		rc.page = string(j)
	}
	page := rc.page
	rc.mu.Unlock()
	out := func(s string) { w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(s)) }
	switch {
	case r.URL.Path == "/lol-gameflow/v1/gameflow-phase":
		out(`"ChampSelect"`)
	case r.URL.Path == "/lol-gameflow/v1/session":
		out(`{"gameData":{"queue":{"id":700,"gameMode":"CLASSIC","type":"CLASH"}}}`)
	case r.URL.Path == "/lol-champ-select/v1/session":
		out(`{"localPlayerCellId":0,"myTeam":[{"cellId":0,"championId":103,"assignedPosition":"middle","spell1Id":4,"spell2Id":14}],"theirTeam":[{"championId":157,"puuid":"e1"}]}`)
	case r.URL.Path == "/lol-game-data/assets/v1/champion-summary.json":
		out(`[{"id":103,"name":"Ahri","alias":"Ahri"},{"id":157,"name":"Yasuo","alias":"Yasuo"}]`)
	case r.URL.Path == "/lol-summoner/v1/current-summoner":
		out(`{"puuid":"me"}`)
	case r.URL.Path == "/lol-clash/v1/player":
		out(`{"rosterId":"r1"}`)
	case r.URL.Path == "/lol-clash/v1/roster/r1":
		out(`{"id":"r1","bracketId":"b","members":[{"puuid":"me","position":"MIDDLE"}]}`)
	case r.URL.Path == "/lol-clash/v1/bracket/b":
		out(`{"matches":[{"roundId":1,"rosterId1":"r1","rosterId2":"r2"}]}`)
	case r.URL.Path == "/lol-clash/v1/roster/r2":
		out(`{"id":"r2","name":"Rivals","members":[{"puuid":"e1","position":"MIDDLE","gameName":"Rival","tagLine":"EUW"}]}`)
	case r.URL.Path == "/lol-clash/v1/scouting/champions":
		out(`[{"puuid":"e1","champions":[{"championId":157,"gameCount":40,"winCount":22}]}]`)
	case r.URL.Path == "/lol-perks/v1/pages" && r.Method == "GET":
		if full {
			out(`[{"id":1,"name":"Mine","isDeletable":true},{"id":2,"name":"Also mine","isDeletable":true}]`)
		} else {
			out(`[{"id":1,"name":"Mine","isDeletable":true}]`)
		}
	case r.URL.Path == "/lol-perks/v1/inventory":
		out(`{"ownedPageCount":2}`)
	case r.URL.Path == "/lol-perks/v1/pages" && r.Method == "POST":
		out(page)
	case r.URL.Path == "/lol-perks/v1/currentpage" && r.Method == "GET":
		out(page)
	default:
		w.WriteHeader(204)
	}
}

func newRecorder(t *testing.T) (*recorder, *Client, func()) {
	rc := &recorder{}
	srv := httptest.NewTLSServer(http.HandlerFunc(rc.serve))
	return rc, NewClientURL(srv.URL, "pw", srv.Client()), srv.Close
}

func (rc *recorder) writes() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	var w []string
	for _, c := range rc.calls {
		if !strings.HasPrefix(c, "GET ") {
			w = append(w, c)
		}
	}
	return w
}

// Vision 1: the app changes nothing in the League client unless the player clicks Apply.
// Everything it does on its own (watching champ select, scouting Clash) only reads.
func TestVisionReadsNeverWrite(t *testing.T) {
	rc, c, done := newRecorder(t)
	defer done()
	_, _ = c.Phase()
	g, _ := c.Game()
	s, _ := c.Session()
	_ = s.Mine()
	_ = s.EnemyPicks()
	ch, _ := c.Champions()
	_, _, err := c.ClashFromClient(c.CurrentPuuid(), ch, nil)
	if err != nil || !g.Clash {
		t.Fatalf("the fake client should be in Clash: %v %+v", err, g)
	}
	if w := rc.writes(); len(w) != 0 {
		t.Fatalf("reading wrote to the client: %v", w)
	}
}

// Vision 2: the app only ever writes your rune page and your summoner spells, nothing else in the client.
func TestVisionOnlyRunesAndSpellsAreWritten(t *testing.T) {
	allowed := map[string]bool{
		"PUT /lol-perks/v1/pages/":                        true, // changes a page in place (never deletes it)
		"POST /lol-perks/v1/pages":                        true,
		"PUT /lol-perks/v1/currentpage":                   true,
		"PATCH /lol-champ-select/v1/session/my-selection": true,
	}
	// every write call in the source, wherever it is
	re := regexp.MustCompile(`\.Do\(\s*"(POST|PUT|PATCH|DELETE)",\s*(?:fmt\.Sprintf\()?"([^"%]*)`)
	files, _ := filepath.Glob("../*.go")
	more, _ := filepath.Glob("*.go")
	files = append(files, more...)
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			found++
			if !allowed[m[1]+" "+m[2]] {
				t.Errorf("%s writes %s %s: only rune pages and summoner spells may be written", f, m[1], m[2])
			}
		}
		if strings.HasSuffix(f, "_windows.go") && regexp.MustCompile(`\.Do\(\s*"(POST|PUT|PATCH|DELETE)"`).Match(src) {
			t.Errorf("%s talks to the client directly; writes belong in core (ApplyRunes, ApplySpells)", f)
		}
	}
	if found != 4 {
		t.Errorf("expected the 4 known write calls, found %d: update this test only if the owner asked for a new kind of change", found)
	}
}

// Vision 3: the name "lømsland" never appears in the League client.
func TestVisionNoAppNameInTheClient(t *testing.T) {
	bad := regexp.MustCompile(`(?i)l[øo]msland|companion`)
	for _, champ := range []string{"Ahri", "Nunu & Willump", "Kai'Sa", "Renata Glasc", "Aurelion Sol", "Dr. Mundo"} {
		for _, role := range append([]string{""}, Roles...) {
			if n := PageName(champ, role); bad.MatchString(n) {
				t.Fatalf("page name %q", n)
			}
		}
	}
	rc, c, done := newRecorder(t)
	defer done()
	b := sampleBuild(t)
	if r := c.ApplyRunes(b.Runes[0], PageName("Ahri", "mid"), nil, 0, false); !r.OK {
		t.Fatal(r)
	}
	if r := c.ApplySpells([]int{4, 14}, nil, 4, 14, false); !r.OK {
		t.Fatal(r)
	}
	for _, w := range rc.writes() {
		if bad.MatchString(w) {
			t.Fatalf("the app's name was sent to the client: %s", w)
		}
	}
}

// Vision 3b: the panel beside the client doesn't show the name either (it sits right next to the client).
func TestVisionNoAppNameInThePanel(t *testing.T) {
	b, err := os.ReadFile("../panel.html")
	if err != nil {
		t.Fatal(err)
	}
	visible := regexp.MustCompile(`(?s)/\*.*?\*/|<!--.*?-->`).ReplaceAllString(string(b), "")
	if m := regexp.MustCompile(`(?i)l[øo]msland`).FindString(visible); m != "" {
		t.Fatalf("panel.html shows %q", m)
	}
	src, _ := os.ReadFile("../main_windows.go")
	if regexp.MustCompile(`(?i)Title:\s*"[^"]*l[øo]msland`).Match(src) {
		t.Fatal("the panel window's title has the app's name")
	}
}

// Vision 3c: the app has a neutral name: no "lømsland" in anything a player sees (tray, messages,
// window titles, errors, file and folder names). The build site's own address is allowed.
func TestVisionNeutralName(t *testing.T) {
	files, _ := filepath.Glob("../*.go")
	more, _ := filepath.Glob("*.go")
	cmd, _ := filepath.Glob("../cmd/*/*.go")
	files = append(append(files, more...), cmd...)
	files = append(files, "../build.sh", "../app.manifest")
	str := regexp.MustCompile("\"(?:[^\"\\\\\n]|\\\\.)*\"|`[^`]*`")
	name := regexp.MustCompile(`(?i)l[øo]msland`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, lit := range str.FindAllString(string(src), -1) {
			if !name.MatchString(lit) {
				continue
			}
			// allowed: the site's address, and the early test build's names, used only to find and migrate it
			cleaned := strings.NewReplacer("xn--lmsland", "", `"lomsland-sign"`, "", `"lomsland-companion"`, "", `"lomsland-companion.exe"`, "").Replace(lit)
			if strings.Contains(f, "apply.go") && lit == `"lømsland"` { // LegacyPrefix: recognising old pages
				continue
			}
			if name.MatchString(cleaned) {
				t.Errorf("%s shows the old name: %s", f, lit)
			}
		}
	}
}

// Vision 4: your own rune pages are never changed unless you pick one to replace.
func TestVisionYourRunePagesAreSafe(t *testing.T) {
	rc, c, done := newRecorder(t)
	defer done()
	rc.full = true
	b := sampleBuild(t)
	r := c.ApplyRunes(b.Runes[0], "Ahri Mid", nil, 0, false)
	if r.OK || len(r.Replace) != 2 || len(rc.writes()) != 0 {
		t.Fatalf("full slots must ask first, changing nothing: %+v %v", r, rc.writes())
	}
	rc.full = false
	if r := c.ApplyRunes(b.Runes[0], "Ahri Mid", []int{999}, 0, false); !r.OK {
		t.Fatal(r)
	}
	for _, w := range rc.writes() {
		if strings.HasPrefix(w, "DELETE") || strings.HasPrefix(w, "PUT /lol-perks/v1/pages/1 ") {
			t.Fatalf("a page that isn't the app's was changed or deleted: %s", w)
		}
	}
	// no blank page: a page is changed in place, and no rune page is ever deleted (no moment without one)
	src, _ := os.ReadFile("apply.go")
	if strings.Contains(string(src), `"DELETE"`) {
		t.Fatal("rune pages must be changed in place, never deleted and made again")
	}
}

// Vision 5: Clash mode only when the client says the game is Clash; suggestions only on Summoner's Rift.
func TestVisionClashOnlyInClash(t *testing.T) {
	cases := []struct {
		g    Game
		want SelectPlan
	}{
		{Game{Mode: "CLASSIC"}, SelectPlan{Panel: true}},
		{Game{Mode: "CLASSIC", Clash: true}, SelectPlan{Panel: true, ClashClient: true, ClashHelper: true}},
		{Game{Mode: "ARAM", Clash: true}, SelectPlan{}},
		{Game{Mode: "ARAM"}, SelectPlan{}},
		{Game{Mode: "CHERRY"}, SelectPlan{}},
		{Game{Mode: "PRACTICETOOL"}, SelectPlan{Panel: true}},
		{Game{Mode: "PRACTICETOOL", Clash: true}, SelectPlan{Panel: true}}, // never Clash outside Summoner's Rift 5v5
		{Game{Mode: ""}, SelectPlan{}},
	}
	for _, c := range cases {
		if got := PlanFor(c.g); got != c.want {
			t.Errorf("%+v: got %+v want %+v", c.g, got, c.want)
		}
	}
	// the app decides through PlanFor, and its Clash readers refuse to run outside Clash
	src, err := os.ReadFile("../main_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, must := range []string{"core.PlanFor(g)", "if plan.ClashClient {", "if plan.ClashHelper {", "if lcu == nil || !clash { // never outside Clash", "if !a.clash {"} {
		if !strings.Contains(s, must) {
			t.Errorf("main_windows.go no longer contains %q", must)
		}
	}
	// the only other way in is the panel's Refresh, which goes through the same guarded loaders
	if strings.Count(s, `go a.loadClash("")`) != 1 {
		t.Error("Clash helper scouting is started from more than one place")
	}
	// completely hidden until Clash: the panel gets Clash data only in a Clash champ select
	if !strings.Contains(s, `if a.clash && a.state != "unsupported" {
		v.Clash = a.cl.view()`) || strings.Count(s, "v.Clash =") != 1 {
		t.Error("the panel must only get Clash data in a Clash game")
	}
	// nothing to choose: the app picks the source itself, and the panel has no drop-down
	p, _ := os.ReadFile("../panel.html")
	if strings.Contains(string(p), "<select") || strings.Contains(s, `w.Bind("clashMatch"`) {
		t.Error("the panel must not have a selector: the app detects the queue and the Clash source itself")
	}
}

// Vision 6: the numbers are EUW Emerald+ from build.lømsland.no, and the panel says so.
func TestVisionEUWFromTheSite(t *testing.T) {
	if SiteURL != "https://build.xn--lmsland-q1a.no" || ClashURL != "https://clash.xn--lmsland-q1a.no" {
		t.Fatal(SiteURL, ClashURL)
	}
	p, _ := os.ReadFile("../panel.html")
	if !strings.Contains(string(p), "EUW · Emerald+") {
		t.Fatal("the panel no longer says where its numbers are from")
	}
}

// Vision 7: Clash scouting only reads from the Clash helper; it never saves or deletes matches there.
func TestVisionClashHelperReadOnly(t *testing.T) {
	var ops []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		ops = append(ops, b["op"].(string))
		if r.Header.Get("x-pass") != "" {
			t.Error("the app must not send the team password")
		}
		_, _ = w.Write([]byte(`{"matches":[],"match":{"id":"x","them":[]}}`))
	}))
	defer srv.Close()
	_, _ = ClashMatches(srv.Client(), srv.URL)
	_, _ = GetClashMatch(srv.Client(), srv.URL, "x")
	sort.Strings(ops)
	if strings.Join(ops, ",") != "get,list" {
		t.Fatal(ops)
	}
}

// Vision 8: updates only install when signed with the owner's key.
func TestVisionUpdatesNeedTheOwnersSignature(t *testing.T) {
	src, err := os.ReadFile("../update_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	verify, write := strings.Index(s, "core.VerifyRelease("), strings.Index(s, "os.WriteFile(newPath")
	if verify < 0 || write < 0 || verify > write {
		t.Fatal("the update must be verified before it is written")
	}
	if !strings.Contains(s, `if updateKey == "" {`) {
		t.Fatal("a build without a key must not update itself")
	}
	if VerifyRelease("", "v1.0.1", []byte(VersionMarker("v1.0.1")), []byte("x")) == nil {
		t.Fatal("an unsigned update was accepted")
	}
}

// Vision 9: the panel can't be made to run code from the data it shows.
func TestVisionPanelIsLockedDown(t *testing.T) {
	b, err := os.ReadFile("../panel.html")
	if err != nil {
		t.Fatal(err)
	}
	p := string(b)
	for _, must := range []string{`http-equiv="Content-Security-Policy"`, `default-src 'none'`, `script-src 'nonce-__NONCE__'`, `<script nonce="__NONCE__">`, `img-src https://ddragon.leagueoflegends.com;`} {
		if !strings.Contains(p, must) {
			t.Errorf("panel.html lost %q", must)
		}
	}
	if m := regexp.MustCompile(`<[^>]+\son[a-z]+\s*=`).FindString(p); m != "" {
		t.Errorf("inline event handler in panel.html: %s", m)
	}
	if m := regexp.MustCompile(`(?i)<script[^>]+src=|fonts\.googleapis|fonts\.gstatic`).FindString(p); m != "" {
		t.Errorf("panel.html loads something from outside: %s", m)
	}
}

// Vision 10: Apply sets spells the way you play: each spell stays on the key it's on (Flash on F stays on F).
func TestVisionSpellsKeepTheirKeys(t *testing.T) {
	spells := []int{1, 3, 4, 6, 7, 11, 12, 14, 21}
	for _, c1 := range spells {
		for _, c2 := range spells {
			for _, a := range spells {
				for _, b := range spells {
					if c1 == c2 || a == b {
						continue
					}
					d, f := SpellSlots(c1, c2, a, b)
					if (a == c1 && d != a) || (a == c2 && f != a) || (b == c1 && d != b) || (b == c2 && f != b) {
						t.Fatalf("current D=%d F=%d, new %d+%d → D=%d F=%d", c1, c2, a, b, d, f)
					}
				}
			}
		}
	}
}

// Vision 11: Apply never leaves you with a blank rune page. The page is changed in place (never deleted), its
// runes are checked against the client first and read back afterwards, and Apply is locked in the last seconds.
func TestVisionNoBlankRunePage(t *testing.T) {
	src, _ := os.ReadFile("apply.go")
	s := string(src)
	for _, must := range []string{`c.Do("PUT", fmt.Sprintf("/lol-perks/v1/pages/%d", target)`, "c.unknownRunes(", "if !c.runesActive(target, body) {"} {
		if !strings.Contains(s, must) {
			t.Errorf("apply.go no longer contains %q", must)
		}
	}
	if strings.Contains(s, `"DELETE"`) {
		t.Error("rune pages must never be deleted and made again")
	}
	app, _ := os.ReadFile("../main_windows.go")
	if strings.Count(string(app), "if late {\n\t\treturn core.Result{Msg: tooLate}") != 2 {
		t.Error("both Apply buttons must refuse in the last seconds before the game starts")
	}
	if !(Timer{Phase: "FINALIZATION", LeftInMs: 2000}).TooLate(time.Now(), time.Now()) {
		t.Error("2 s before the game starts must count as too late")
	}
}

// Vision 12: the in-game card stays within Riot's rules. It shows only the plan from champ select and your own
// level: it asks the game for nothing else, shows nothing about other players, never sends input to the game,
// and only claims its hotkey while a game with a card runs.
func TestVisionInGameCardIsReferenceOnly(t *testing.T) {
	live, _ := os.ReadFile("livegame.go")
	if n := strings.Count(string(live), "/liveclientdata/"); n != 1 || !strings.Contains(string(live), `"/liveclientdata/activeplayer"`) {
		t.Errorf("the app may only ask the game for your own player (found %d live-data paths)", n)
	}
	if strings.Contains(string(live), "http.MethodPost") || strings.Contains(string(live), `"POST"`) {
		t.Error("the in-game reader must only read")
	}
	app, _ := os.ReadFile("../main_windows.go")
	s := string(app)
	i := strings.Index(s, "func (a *App) view() view {\n\tif a.inGame {")
	j := strings.Index(s, "\tv := view{")
	if i < 0 || j < i {
		t.Fatal("view() must start with the in-game card")
	}
	for _, bad := range []string{"Matchup", "Clash", "enemies", "Counter", "posOf", "cl."} {
		if strings.Contains(s[i:j], bad) {
			t.Errorf("the in-game card must not show %s", bad)
		}
	}
	if !strings.Contains(s, "return a.cardArmed && a.build != nil") || !strings.Contains(s, "a.cardArmed = a.gate.Update(phase)") {
		t.Error("the card must only show in a game entered straight from your own champ select (not TFT, spectating, after a dodge)")
	}
	if !strings.Contains(s, "want := a.inGame && a.cardReady()") || strings.Count(s, "cardKeysDown()") != 1 {
		t.Error("the card's keys must only be looked at while a game with a card runs")
	}
	for _, f := range []string{"../main_windows.go", "../win_windows.go", "../tray_windows.go", "../update_windows.go"} {
		b, _ := os.ReadFile(f)
		for _, input := range []string{"SendInput", "keybd_event", "mouse_event", "SetCursorPos", "SetWindowsHookEx", "RegisterRawInputDevices"} {
			if strings.Contains(string(b), input) {
				t.Errorf("%s sends input (%s): the app never plays for you", f, input)
			}
		}
	}
	w, _ := os.ReadFile("../win_windows.go")
	if !strings.Contains(string(w), "ex |= wsExTransparent") || !strings.Contains(string(w), "wsExLayered | wsExNoActivate") {
		t.Error("the card must not take focus, and must let clicks through to the game")
	}
}
