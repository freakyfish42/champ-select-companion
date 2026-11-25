package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func healthServers(t *testing.T, latest string, clashOps *[]string) (*httptest.Server, *httptest.Server, *httptest.Server) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tierlist" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"roles":{},"opggAt":` + jsonNum(now.Add(-3*time.Hour).UnixMilli()) + `}`))
	}))
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		*clashOps = append(*clashOps, b["op"].(string))
		_, _ = w.Write([]byte(`{"matches":[{"id":"a","name":"A","updated":1},{"id":"b","name":"B","updated":2}]}`))
	}))
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if latest == "" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"` + latest + `"}`))
	}))
	return site, clash, gh
}

func deps(t *testing.T, lcu *httptest.Server, site, clash, gh *httptest.Server, version string, key bool) HealthDeps {
	return HealthDeps{
		FindLeague: func() (Creds, error) {
			if lcu == nil {
				return Creds{}, errors.New("League isn't running")
			}
			return Creds{Port: 51234, Password: "pw"}, nil
		},
		NewClient: func(Creds) *Client { return NewClientURL(lcu.URL, "pw", lcu.Client()) },
		Web:       site.Client(), SiteURL: site.URL, ClashURL: clash.URL, LatestAPI: gh.URL,
		Version: version, HasKey: key,
		WebView2:  func() (string, error) { return "131.0.2903.70", nil },
		Autostart: func() bool { return true },
		Now:       func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) },
		ShowPanel: true,
	}
}

func TestHealthAllGood(t *testing.T) {
	var ops []string
	site, clash, gh := healthServers(t, "v1.0.0", &ops)
	defer site.Close()
	defer clash.Close()
	defer gh.Close()
	rc, _, done := newRecorder(t)
	defer done()
	srv := httptest.NewTLSServer(http.HandlerFunc(rc.serve))
	defer srv.Close()
	cs := Health(deps(t, srv, site, clash, gh, "v1.0.0", true))
	sum := Summary(cs)
	for _, want := range []string{"✓  League client: Connected (port 51234, now: champ select)", "✓  Build site: Reachable · data updated 3 h ago", "✓  Clash helper: Reachable · 2 saved matches", "✓  Panel: WebView2 131.0.2903.70 installed", "✓  Settings: Panel on, test mode off", "✓  Updates: You have the latest version (v1.0.0)", "✓  Start with Windows: On"} {
		if !strings.Contains(sum, want) {
			t.Errorf("missing %q in\n%s", want, sum)
		}
	}
	if Worst(cs) != CheckOK {
		t.Fatal(Worst(cs))
	}
	// Vision: the setup check only reads
	if w := rc.writes(); len(w) != 0 {
		t.Fatalf("the setup check wrote to the client: %v", w)
	}
	if strings.Join(ops, ",") != "list" {
		t.Fatal(ops)
	}
	r := Report(cs, "v1.0.0", time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC), "line 1\nline 2")
	if !strings.HasPrefix(r, "Champ Select Companion v1.0.0 · setup check 2026-11-25 12:00") || !strings.HasSuffix(r, "line 1\nline 2") {
		t.Fatal(r)
	}
}

func TestHealthProblems(t *testing.T) {
	var ops []string
	site, clash, gh := healthServers(t, "v1.0.3", &ops)
	defer clash.Close()
	defer gh.Close()
	site.Close() // the site is down
	d := deps(t, nil, site, clash, gh, "v1.0.0", true)
	d.Web = clash.Client()
	d.WebView2 = func() (string, error) { return "", errors.New("not found") }
	d.Autostart = func() bool { return false }
	cs := Health(d)
	sum := Summary(cs)
	for _, want := range []string{"!  League client: Not running. Start League and check again.", "✗  Build site: Can't reach it", "✗  Panel: Microsoft Edge WebView2 is missing", "!  Updates: v1.0.3 is out (you have v1.0.0)", "!  Start with Windows: Off"} {
		if !strings.Contains(sum, want) {
			t.Errorf("missing %q in\n%s", want, sum)
		}
	}
	if Worst(cs) != CheckFail {
		t.Fatal(Worst(cs))
	}
	// no releases yet, and a build without a key
	gh2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer gh2.Close()
	d.LatestAPI = gh2.URL
	if s := Summary(Health(d)); !strings.Contains(s, "!  Updates: Couldn't check: no releases published yet") {
		t.Fatal(s)
	}
	gh3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`)) }))
	defer gh3.Close()
	d.LatestAPI, d.HasKey = gh3.URL, false
	if s := Summary(Health(d)); !strings.Contains(s, "no update key") {
		t.Fatal(s)
	}
}

func TestHealthSettingsAndPanel(t *testing.T) {
	var ops []string
	site, clash, gh := healthServers(t, "v1.0.0", &ops)
	defer site.Close()
	defer clash.Close()
	defer gh.Close()
	d := deps(t, nil, site, clash, gh, "v1.0.0", true)
	d.ShowPanel = false
	if s := Summary(Health(d)); !strings.Contains(s, "!  Settings: The panel is turned off: tick") {
		t.Fatal(s)
	}
	d.ShowPanel, d.TestMode = true, true
	if s := Summary(Health(d)); !strings.Contains(s, "!  Settings: Test mode is on, so Apply changes nothing") {
		t.Fatal(s)
	}
	d.TestMode, d.PanelError = false, "could not create the window"
	cs := Health(d)
	if s := Summary(cs); !strings.Contains(s, "✗  Panel: Didn't start: could not create the window") || Worst(cs) != CheckFail {
		t.Fatal(s)
	}
	// a fixed order, whichever check finishes first
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "League client,Build site,Clash helper,Panel,Settings,Updates,Start with Windows" {
		t.Fatal(names)
	}
}

// the checks run at the same time: three slow services cost one wait, not three
func TestHealthRunsInParallel(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		if r.URL.Path == "/api/tierlist" {
			_, _ = w.Write([]byte(`{"roles":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"matches":[],"tag_name":"v1.0.0"}`))
	}))
	defer slow.Close()
	d := deps(t, nil, slow, slow, slow, "v1.0.0", true)
	start := time.Now()
	cs := Health(d)
	if el := time.Since(start); el > 1000*time.Millisecond {
		t.Fatalf("took %v: the checks ran one after another", el)
	}
	if Worst(cs) == CheckFail {
		t.Fatal(Summary(cs))
	}
}
