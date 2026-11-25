package core

// The setup check behind the tray's "Check setup": a few read-only tests that show at a glance
// why the panel doesn't appear, and a report a teammate can paste to the owner.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CheckState int

const (
	CheckOK   CheckState = iota // ✓
	CheckWarn                   // ! fine for now, but worth knowing
	CheckFail                   // ✗ something needs fixing
)

type Check struct {
	Name   string
	State  CheckState
	Detail string
}

// HealthDeps is everything the checks need, so they can be tested without Windows.
type HealthDeps struct {
	FindLeague func() (Creds, error)
	NewClient  func(Creds) *Client
	Web        *http.Client
	SiteURL    string
	ClashURL   string
	LatestAPI  string // GitHub's latest-release address
	Version    string
	HasKey     bool                   // an update key is built in
	WebView2   func() (string, error) // the installed WebView2 version
	Autostart  func() bool
	Now        func() time.Time
	ShowPanel  bool   // the tray's "Show panel in champ select"
	TestMode   bool   // the tray's "Test mode"
	PanelError string // set when the panel window failed to start in this run
}

// Health runs the checks at the same time (the slowest one sets the wait, about 10 s at most) and
// lists them in a fixed order. They only read: nothing is changed in the client, the site or the Clash helper.
func Health(d HealthDeps) []Check {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	mk := func(name string, s CheckState, detail string, args ...any) Check {
		return Check{name, s, fmt.Sprintf(detail, args...)}
	}
	jobs := []func() Check{
		func() Check { // 1. the League client
			creds, err := d.FindLeague()
			switch {
			case err != nil && strings.Contains(err.Error(), "isn't running"):
				return mk("League client", CheckWarn, "Not running. Start League and check again.")
			case err != nil:
				return mk("League client", CheckWarn, "Not connected: %s. Restart League and check again.", err)
			}
			ph, err := d.NewClient(creds).Phase()
			if err != nil {
				return mk("League client", CheckFail, "Found, but can't connect: %s", err)
			}
			return mk("League client", CheckOK, "Connected (port %d, now: %s)", creds.Port, phaseName(ph))
		},
		func() Check { // 2. the build site
			t, err := FetchTierList(d.Web, d.SiteURL)
			if err != nil {
				return mk("Build site", CheckFail, "Can't reach it: %s", err)
			}
			at := t.OpggAt
			if at == 0 {
				at = t.Updated
			}
			if at <= 0 {
				return mk("Build site", CheckOK, "Reachable")
			}
			h := now().Sub(time.UnixMilli(at)).Hours()
			st := CheckOK
			if h > 48 {
				st = CheckWarn
			}
			return mk("Build site", st, "Reachable · data updated %s", ago(h))
		},
		func() Check { // 3. the Clash helper (only used in Clash)
			ms, err := ClashMatches(d.Web, d.ClashURL)
			if err != nil {
				return mk("Clash helper", CheckWarn, "Can't read saved matches: %s (only needed in Clash)", err)
			}
			return mk("Clash helper", CheckOK, "Reachable · %d saved match%s", len(ms), plural(len(ms), "es"))
		},
		func() Check { // 4. the panel needs WebView2, and must have started
			v, err := d.WebView2()
			switch {
			case err != nil || v == "":
				return mk("Panel", CheckFail, "Microsoft Edge WebView2 is missing, so the panel can't open. Install it from Microsoft and restart.")
			case d.PanelError != "":
				return mk("Panel", CheckFail, "Didn't start: %s. Quit and start the app again.", d.PanelError)
			}
			return mk("Panel", CheckOK, "WebView2 %s installed", v)
		},
		func() Check { // 5. the app's own settings: the usual reasons for "nothing shows" or "Apply does nothing"
			switch {
			case !d.ShowPanel && d.TestMode:
				return mk("Settings", CheckWarn, "The panel is turned off and test mode is on: fix both in the tray menu")
			case !d.ShowPanel:
				return mk("Settings", CheckWarn, "The panel is turned off: tick “Show panel in champ select” in the tray menu")
			case d.TestMode:
				return mk("Settings", CheckWarn, "Test mode is on, so Apply changes nothing: untick it in the tray menu")
			}
			return mk("Settings", CheckOK, "Panel on, test mode off")
		},
		func() Check { // 6. updates
			tag, err := latestTag(d.Web, d.LatestAPI)
			switch {
			case d.Version == "dev":
				return mk("Updates", CheckWarn, "Development build: doesn't update itself")
			case err != nil:
				return mk("Updates", CheckWarn, "Couldn't check: %s", err)
			case Newer(d.Version, tag):
				return mk("Updates", CheckWarn, "%s is out (you have %s): use Check for updates", tag, d.Version)
			case !d.HasKey:
				return mk("Updates", CheckWarn, "You have the latest (%s), but this build has no update key, so it won't update itself", d.Version)
			}
			return mk("Updates", CheckOK, "You have the latest version (%s)", d.Version)
		},
		func() Check { // 7. start with Windows
			if d.Autostart() {
				return mk("Start with Windows", CheckOK, "On")
			}
			return mk("Start with Windows", CheckWarn, "Off: turn it on in the tray menu, or the app won't be there next time")
		},
	}
	out := make([]Check, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = job()
		}()
	}
	wg.Wait()
	return out
}

func phaseName(p string) string {
	switch p {
	case "None":
		return "home screen"
	case "ChampSelect":
		return "champ select"
	case "InProgress":
		return "in a game"
	}
	return strings.ToLower(p)
}

func ago(h float64) string {
	switch {
	case h < 1.5:
		return fmt.Sprintf("%d min ago", int(h*60+0.5))
	case h < 48:
		return fmt.Sprintf("%d h ago", int(h+0.5))
	}
	return fmt.Sprintf("%d days ago", int(h/24+0.5))
}

func plural(n int, s string) string {
	if n == 1 {
		return ""
	}
	return s
}

func latestTag(hc *http.Client, api string) (string, error) {
	req, _ := http.NewRequest("GET", api, nil)
	req.Header.Set("User-Agent", "champ-select-companion")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return "", fmt.Errorf("no releases published yet")
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("GitHub answered %s", res.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&r)
	return r.Tag, err
}

// Summary is the short text for the check window: one line per check, worst first marked.
func Summary(cs []Check) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString([]string{"✓", "!", "✗"}[c.State] + "  " + c.Name + ": " + c.Detail + "\n")
	}
	return b.String()
}

// Report is the full text to paste to the owner: version, the checks and the end of the log.
func Report(cs []Check, version string, at time.Time, logTail string) string {
	return "Champ Select Companion " + version + " · setup check " + at.Format("2006-01-02 15:04") + "\n\n" +
		Summary(cs) + "\nLast lines of log.txt:\n" + logTail
}

// Worst is the most serious state among the checks.
func Worst(cs []Check) CheckState {
	w := CheckOK
	for _, c := range cs {
		if c.State > w {
			w = c.State
		}
	}
	return w
}
