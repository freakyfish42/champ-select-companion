package core

// The setup check behind the tray's "Check setup": a few read-only tests that show at a glance
// why the panel doesn't appear, and a report a teammate can paste to the owner.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
}

// Health runs the checks. They only read: nothing is changed in the client, the site or the Clash helper.
func Health(d HealthDeps) []Check {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	var out []Check
	add := func(name string, s CheckState, detail string, args ...any) {
		out = append(out, Check{name, s, fmt.Sprintf(detail, args...)})
	}

	// 1. the League client
	if creds, err := d.FindLeague(); err != nil {
		if strings.Contains(err.Error(), "isn't running") {
			add("League client", CheckWarn, "Not running. Start League and check again.")
		} else {
			add("League client", CheckWarn, "Not connected: %s. Restart League and check again.", err)
		}
	} else if ph, err := d.NewClient(creds).Phase(); err != nil {
		add("League client", CheckFail, "Found, but can't connect: %s", err)
	} else {
		add("League client", CheckOK, "Connected (port %d, now: %s)", creds.Port, phaseName(ph))
	}

	// 2. the build site
	if t, err := FetchTierList(d.Web, d.SiteURL); err != nil {
		add("Build site", CheckFail, "Can't reach it: %s", err)
	} else {
		at := t.OpggAt
		if at == 0 {
			at = t.Updated
		}
		if at > 0 {
			h := now().Sub(time.UnixMilli(at)).Hours()
			st := CheckOK
			if h > 48 {
				st = CheckWarn
			}
			add("Build site", st, "Reachable · data updated %s", ago(h))
		} else {
			add("Build site", CheckOK, "Reachable")
		}
	}

	// 3. the Clash helper (only used in Clash)
	if ms, err := ClashMatches(d.Web, d.ClashURL); err != nil {
		add("Clash helper", CheckWarn, "Can't read saved matches: %s (only needed in Clash)", err)
	} else {
		add("Clash helper", CheckOK, "Reachable · %d saved match%s", len(ms), plural(len(ms), "es"))
	}

	// 4. the panel needs WebView2
	if v, err := d.WebView2(); err != nil || v == "" {
		add("Panel", CheckFail, "Microsoft Edge WebView2 is missing, so the panel can't open. Install it from Microsoft and restart.")
	} else {
		add("Panel", CheckOK, "WebView2 %s installed", v)
	}

	// 5. updates
	switch tag, err := latestTag(d.Web, d.LatestAPI); {
	case d.Version == "dev":
		add("Updates", CheckWarn, "Development build: doesn't update itself")
	case err != nil:
		add("Updates", CheckWarn, "Couldn't check: %s", err)
	case Newer(d.Version, tag):
		add("Updates", CheckWarn, "%s is out (you have %s): use Check for updates", tag, d.Version)
	case !d.HasKey:
		add("Updates", CheckWarn, "You have the latest (%s), but this build has no update key, so it won't update itself", d.Version)
	default:
		add("Updates", CheckOK, "You have the latest version (%s)", d.Version)
	}

	// 6. start with Windows
	if d.Autostart() {
		add("Start with Windows", CheckOK, "On")
	} else {
		add("Start with Windows", CheckWarn, "Off: turn it on in the tray menu, or the app won't be there next time")
	}
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
