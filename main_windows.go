// Champ Select Companion: a tray app that watches League's champ select and shows a small panel
// beside the client with the build site's rune pages and summoner spells, applied on click.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freakyfish42/champ-select-companion/core"
	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

var version = "dev" // set at build time: -ldflags "-X main.version=v1.0.0"

// versionMarker puts the version inside the exe where the signing tool can find it ("LMVER:v1.0.0:LMVER").
var versionMarker = "LMVER:dev:LMVER"

//go:embed panel.html
var panelHTML string

//go:embed icon.ico
var iconICO []byte

//go:embed fonts/*.woff2
var fontFS embed.FS

// The panel's fonts (Cinzel and Inter, SIL Open Font License) are built in, so the panel loads nothing from Google.
func panelPage() string {
	css := ""
	for _, f := range []struct{ file, family, weight string }{{"cinzel-700", "Cinzel", "700"}, {"inter-400", "Inter", "400"}, {"inter-600", "Inter", "600"}} {
		b, err := fontFS.ReadFile("fonts/" + f.file + ".woff2")
		if err != nil {
			continue
		}
		css += "@font-face{font-family:" + f.family + ";font-weight:" + f.weight + ";font-display:block;src:url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString(b) + ") format('woff2')}\n"
	}
	nonce := make([]byte, 18)
	_, _ = rand.Read(nonce)
	page := strings.Replace(panelHTML, "/*FONTS*/", css, 1)
	return strings.ReplaceAll(page, "__NONCE__", base64.RawURLEncoding.EncodeToString(nonce))
}

const panelW, panelH = 300, 700

type Config struct {
	ShowPanel bool  `json:"showPanel"`
	TestMode  bool  `json:"testMode"`
	OwnPages  []int `json:"ownPages,omitempty"` // rune pages this app wrote (one per League account), known by id
	HideCard  bool  `json:"hideCard,omitempty"` // the tray's "Show build card in game" is off
	CardDX    int32 `json:"cardDX,omitempty"`   // where the player moved the card: offset from the game's top-left
	CardDY    int32 `json:"cardDY,omitempty"`
	CardAlpha int   `json:"cardAlpha,omitempty"` // the card's opacity in percent: 100, 80 or 60 (0 = default 60)
	CardSize  int   `json:"cardSize,omitempty"`  // the card's size in percent: 100, 125 or 150 (0 = default 125)
	// where the game's Q and R icons are, as the player lined them up (nil = not: the app works it out from
	// League's HUD Scale)
	SkillSpot *core.SkillSpot `json:"skillSpot,omitempty"`
	MarkerOff bool            `json:"markerOff,omitempty"` // the tray's "Skill marker on the ability bar" is off
	// the tray's "Objective countdown" is off
	ObjectivesOff bool `json:"objectivesOff,omitempty"`
}

// cardAlpha: the card's opacity setting (default 60 %).
func (c Config) cardAlpha() int {
	switch c.CardAlpha {
	case 100, 80, 60:
		return c.CardAlpha
	}
	return 60
}

// cardSize: the card's size setting (default 125 %).
func (c Config) cardSize() int {
	switch c.CardSize {
	case 100, 125, 150:
		return c.CardSize
	}
	return 125
}

type App struct {
	dir   string
	prefs *core.Prefs
	web   *http.Client

	mu          sync.Mutex
	cfg         Config
	lcu         *core.Client
	champs      map[int]core.Champion
	banned      []int // champions banned so far in this champ select
	inSelect    bool
	phase       string
	mode        string
	inGame      bool                // a game is loading or running
	level       int                 // your champion's level in that game, from Riot's Live Client Data API (0 = not loaded yet)
	skills      [4]int              // your points in Q W E R, from the same place
	cardHidden  bool                // hidden with the hotkey, for this game
	moving      bool                // the player is moving the card (it takes clicks while moving)
	liningUp    bool                // the player is lining up the skill marker over the ability bar
	onLiningEnd func()              // set by the tray: lining up ended by itself (the game ended)
	live        *core.Live          // Riot's Live Client Data API (your own player and the game clock)
	jgCache     map[string]*jgEntry // first clears per champion (yours, and the enemy jungler's in champ select)
	needJg      []string            // set by view(): champions whose first clears to (re)load
	clock       float64             // the game clock at clockAt (objective countdown), 0 before the first read
	clockAt     time.Time
	hud         float64 // League's HUD Scale (0-1) from game.cfg, read every few seconds in game
	hudOK       bool
	cardH       int32         // the card's height as the page measured it (CSS px)
	cardDPR     float64       // the page's own pixel scale (window.devicePixelRatio): sizes and pointer use this
	gate        core.CardGate // does this game get the card? Only one entered straight from your own champ select
	cardArmed   bool
	timer       core.Timer // champ select's clock, read at timerAt
	timerAt     time.Time
	queue       string // the queue the client says this champ select is for, e.g. "Ranked Solo/Duo"
	clash       bool
	enemies     []core.EnemyPick     // champions the other team has shown
	posOf       map[string]string    // Clash: the opponents' registered positions, by puuid
	gen         int                  // counts champ selects, so late answers from an earlier one are dropped
	needCnt     string               // set by view(): a lane opponent's build to fetch for counter-pick ideas
	needCnts    []string             // set by view(): the enemy picks' builds (their matchups give the counters on hover)
	cntNext     map[string]time.Time // when a counter-pick fetch may be tried (again)
	tier        *core.TierList
	tierAt      time.Time
	cl          clashState
	pick        core.Pick
	champ       core.Champion
	key         string
	build       *core.Build
	state       string // waiting, pick, loading, build, unsupported, error
	msg         string
	seq         int
	cache       map[string]cached
	applied     map[string]core.Pref
	hidden      bool // closed by the player for this champ select
	dragged     bool // moved by the player for this champ select
	lastSent    string
	status      string
	panelErr    string // why the panel window didn't start, for Check setup
	onStatus    func(string)
	w           webview.WebView
	hwnd        uintptr
	uiWanted    chan struct{}
	uiOnce      sync.Once
}

// clashState: scouting from the League client (this round's opponent) and from the Clash helper's saved matches.
type clashState struct {
	Helper        *core.Scout // the newest saved match in the Clash helper (the fallback)
	Client        *core.Scout // this round's opponent from the League client (preferred)
	Loading       bool
	Err           string
	ClientLoading bool
	ClientErr     string
	seq           int
}

// clashView: what the panel shows. There is nothing to choose: the League client's data when it has
// this round's opponent, otherwise the newest match saved in the Clash helper, named so you can tell.
// banView: one champion in the panel's "Most banned" row; Taken = already banned in this champ select.
type banView struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Ban   float64 `json:"ban"`
	Taken bool    `json:"taken,omitempty"`
}

type clashView struct {
	Scout   *core.Scout `json:"scout,omitempty"`
	Source  string      `json:"source"` // "client", "helper" or ""
	Loading bool        `json:"loading"`
	Err     string      `json:"err,omitempty"`
}

func (c clashState) view() *clashView {
	v := &clashView{Loading: c.Loading || c.ClientLoading}
	switch {
	case c.Client != nil:
		v.Scout, v.Source = c.Client, "client"
	case c.Helper != nil:
		v.Scout, v.Source = c.Helper, "helper"
	case !v.Loading && c.Err != "":
		v.Err = c.Err
	}
	return v
}

type cached struct {
	b  *core.Build
	at time.Time
}

func main() {
	setDPIAware()
	dir := appDir()
	_ = os.MkdirAll(dir, 0o755)
	setupLog(dir)
	args := strings.Join(os.Args[1:], " ")
	log.Printf("start %s (%s) %s", version, versionMarker, args)

	if !strings.Contains(args, "--portable") && !isInstalledCopy(dir) {
		install(dir)
		return
	}
	mutex, ok := singleInstance()
	waits := strings.Contains(args, "--updated") || strings.Contains(args, "--installed") || strings.Contains(args, "--restart")
	for i := 0; !ok && waits && i < 30; i++ {
		time.Sleep(500 * time.Millisecond) // the old version is closing
		mutex, ok = singleInstance()
	}
	if !ok {
		if !strings.Contains(args, "--autostart") && !waits {
			messageBox("Champ Select Companion is already running. Look for its icon in the tray, next to the clock.", mbIconInfo)
		}
		return
	}
	_ = mutex
	cleanupOld(dir)
	migrateOld(dir)

	a := &App{dir: dir, prefs: core.LoadPrefs(filepath.Join(dir, "picks.json")), web: &http.Client{Timeout: 10 * time.Second},
		cfg: Config{ShowPanel: true}, cache: map[string]cached{}, applied: map[string]core.Pref{}, uiWanted: make(chan struct{}, 1), state: "waiting"}
	a.loadConfig()
	go runTray(a)
	go a.watch()
	go a.updateLoop()
	go a.hotkeyLoop()
	a.uiLoop() // the panel's window lives on this (the main) thread
}

func appDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "champ-select-companion")
}

func setupLog(dir string) {
	p := filepath.Join(dir, "log.txt")
	if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(p, p+".1")
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
}

func (a *App) loadConfig() {
	if b, err := os.ReadFile(filepath.Join(a.dir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.cfg)
	}
}

func (a *App) saveConfig() {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.cfg, "", " ")
	a.mu.Unlock()
	_ = os.WriteFile(filepath.Join(a.dir, "settings.json"), b, 0o644)
}

func (a *App) setStatus(s string) {
	a.mu.Lock()
	changed := a.status != s
	a.status = s
	f := a.onStatus
	a.mu.Unlock()
	if changed && f != nil {
		f(s)
	}
}

// ---- watching the League client ----

func inGamePhase(p string) bool { return core.InGamePhase(p) }

func (a *App) watch() {
	var lcu *core.Client
	live := core.NewLive()
	a.mu.Lock()
	a.live = live
	a.mu.Unlock()
	fails := 0
	for {
		if lcu == nil {
			c, err := findLeague()
			if err != nil {
				a.setStatus("Waiting for League")
				a.leaveSelect("")
				time.Sleep(4 * time.Second)
				continue
			}
			lcu = core.NewClient(c)
			a.mu.Lock()
			a.lcu, a.champs = lcu, nil
			a.mu.Unlock()
			a.setStatus("Connected to League")
			log.Printf("connected to the League client on port %d", c.Port)
		}
		phase, err := lcu.Phase()
		if err != nil {
			if fails++; fails >= 3 {
				log.Printf("lost the League client: %v", err)
				lcu, fails = nil, 0
				a.mu.Lock()
				a.lcu = nil
				a.mu.Unlock()
			}
			time.Sleep(2 * time.Second)
			continue
		}
		fails = 0
		a.mu.Lock()
		a.cardArmed = a.gate.Update(phase)
		a.mu.Unlock()
		if inGamePhase(phase) {
			a.leaveSelect(phase)
			if a.enterGame() {
				if p, err := live.Player(); err == nil {
					a.setPlayer(p)
				}
				time.Sleep(time.Second)
			} else {
				time.Sleep(2 * time.Second)
			}
			continue
		}
		a.leaveGame()
		if phase != "ChampSelect" {
			a.leaveSelect(phase)
			time.Sleep(2 * time.Second)
			continue
		}
		a.mu.Lock()
		first := !a.inSelect
		needChamps := a.champs == nil
		a.mu.Unlock()
		if first {
			g, err := lcu.Game()
			if err != nil || g.Mode == "" {
				g.Mode = "CLASSIC" // unknown: assume Summoner's Rift rather than hide the suggestions
			}
			a.enterSelect(g)
		}
		if needChamps {
			if ch, err := lcu.Champions(); err == nil {
				a.mu.Lock()
				a.champs = ch
				a.mu.Unlock()
			}
		}
		if s, err := lcu.Session(); err == nil {
			a.mu.Lock()
			a.enemies = s.EnemyPicks()
			a.banned = s.Bans()
			a.timer, a.timerAt = s.Timer, time.Now()
			a.mu.Unlock()
			a.onPick(s.Mine())
		}
		time.Sleep(600 * time.Millisecond)
	}
}

func (a *App) enterSelect(g core.Game) {
	mode, plan := g.Mode, core.PlanFor(g)
	a.mu.Lock()
	a.gen++
	a.inSelect, a.phase, a.mode, a.queue, a.clash, a.enemies, a.posOf = true, "ChampSelect", mode, g.Queue, plan.ClashClient, nil, nil
	a.inGame, a.moving, a.level, a.skills = false, false, 0, [4]int{}
	a.cl = clashState{}
	a.timer, a.timerAt = core.Timer{}, time.Time{}
	a.hidden, a.dragged = false, false
	a.applied = map[string]core.Pref{}
	a.key, a.build, a.state, a.msg = "", nil, "waiting", ""
	a.banned = nil
	show := a.cfg.ShowPanel
	a.mu.Unlock()
	a.endLiningUp()
	log.Printf("champ select (%s, %s, clash=%v)", g.Queue, mode, plan.ClashClient)
	go a.loadTier()
	if plan.ClashClient { // Clash mode only when the client says this is a Clash game (core.PlanFor)
		go a.loadClientClash()
	}
	if plan.ClashHelper {
		go a.loadClash("")
	}
	if show {
		a.uiOnce.Do(func() { a.uiWanted <- struct{}{} })
	}
	a.push()
}

// cardReady: is there a build card to show in game? Only for the champion you had in champ select, on the Rift.
func (a *App) cardReady() bool {
	return a.cardArmed && a.build != nil && a.state == "build" && core.SupportedMode(a.mode) && !a.cfg.HideCard
}

// enterGame switches the window to the in-game card; it returns whether there is a card (then the level is read).
func (a *App) enterGame() bool {
	a.mu.Lock()
	first := !a.inGame
	if first {
		a.inGame, a.level, a.skills, a.cardHidden, a.moving = true, 0, [4]int{}, false, false
	}
	ok := a.cardReady()
	name := a.champ.Name
	a.mu.Unlock()
	if first {
		log.Printf("game started (card: %v, %s)", ok, name)
		if ok { // the window may not exist yet if the panel was turned off in champ select
			a.uiOnce.Do(func() { a.uiWanted <- struct{}{} })
		}
		go a.forcePush()
	}
	return ok
}

func (a *App) leaveGame() {
	a.mu.Lock()
	was := a.inGame
	a.inGame, a.moving, a.level, a.skills = false, false, 0, [4]int{}
	a.mu.Unlock()
	a.endLiningUp()
	if was {
		log.Printf("game ended")
		go a.forcePush()
	}
}

func (a *App) setPlayer(p core.Player) {
	a.mu.Lock()
	changed := p.Level != a.level || p.Skills != a.skills
	a.level, a.skills = p.Level, p.Skills
	a.mu.Unlock()
	if changed {
		go a.push()
	}
}

// toggleCard: the hotkey.
func (a *App) toggleCard() {
	a.mu.Lock()
	a.cardHidden = !a.cardHidden
	a.mu.Unlock()
}

// hotkeyLoop watches Ctrl+Shift+B while a game with a card runs. League blocks normal Windows hotkeys while the
// game has focus, so the app reads those three keys' state itself (it doesn't hook the keyboard, doesn't see any
// other key, and only looks while a game with a card is running).
func (a *App) hotkeyLoop() {
	was := false
	for {
		a.mu.Lock()
		want := a.inGame && a.cardReady()
		a.mu.Unlock()
		if !want {
			was = false
			time.Sleep(300 * time.Millisecond)
			continue
		}
		down := cardKeysDown()
		if down && !was {
			a.toggleCard()
		}
		was = down
		time.Sleep(50 * time.Millisecond)
	}
}

// setMoving: the tray's "Move the in-game card", and the card's Done button.
func (a *App) setMoving(on bool) {
	a.mu.Lock()
	in := a.inGame && a.cardReady()
	if on && !in {
		a.mu.Unlock()
		messageBox("The build card can be moved while a game is running and the card shows. Start a game (the Practice Tool works), then choose “Move the in-game card” again.", mbIconInfo)
		return
	}
	a.moving = on
	if on {
		a.cardHidden = false
	}
	h := a.hwnd
	a.mu.Unlock()
	if !on && h != 0 {
		if g := gameWindow(); g != 0 {
			cr, ok1 := windowRect(h)
			gr, ok2 := windowRect(g)
			if ok1 && ok2 {
				a.mu.Lock()
				a.cfg.CardDX, a.cfg.CardDY = cr.Left-gr.Left, cr.Top-gr.Top
				if a.cfg.CardDX == 0 && a.cfg.CardDY == 0 {
					a.cfg.CardDX = 1 // 0,0 means "default place"
				}
				a.mu.Unlock()
				a.saveConfig()
			}
		}
	}
	go a.forcePush()
}

func (a *App) setCardAlpha(pct int) {
	a.mu.Lock()
	a.cfg.CardAlpha = pct
	a.mu.Unlock()
	a.saveConfig()
	go a.forcePush()
}

func (a *App) setCardSize(pct int) {
	a.mu.Lock()
	a.cfg.CardSize = pct
	a.mu.Unlock()
	a.saveConfig()
	go a.forcePush()
}

func (a *App) setShowCard(on bool) {
	a.mu.Lock()
	a.cfg.HideCard = !on
	a.mu.Unlock()
	a.saveConfig()
	go a.forcePush()
}

func (a *App) leaveSelect(phase string) {
	a.mu.Lock()
	a.phase = phase
	was := a.inSelect
	a.inSelect = false
	a.mu.Unlock()
	if was {
		log.Printf("left champ select (%s)", phase)
	}
}

func (a *App) onPick(p core.Pick) {
	a.mu.Lock()
	a.pick = p
	switch {
	case !core.SupportedMode(a.mode):
		a.state = "unsupported"
	case p.ChampionID <= 0:
		a.key, a.build, a.state = "", nil, "pick"
	default:
		ch, ok := a.champs[p.ChampionID]
		if !ok {
			a.state = "pick"
			break
		}
		key := ch.Alias + ":" + p.Role
		if key != a.key {
			a.key, a.champ, a.build = key, ch, nil
			if c, ok := a.cache[key]; ok && time.Since(c.at) < 30*time.Minute {
				a.build, a.state = c.b, "build"
			} else {
				a.state = "loading"
				a.seq++
				go a.fetch(a.seq, key, ch, p.Role)
			}
		}
	}
	a.mu.Unlock()
	a.push()
}

func (a *App) fetch(seq int, key string, ch core.Champion, role string) {
	b, err := core.FetchBuild(a.web, core.SiteURL, ch.Alias, role)
	a.mu.Lock()
	if err == nil {
		a.remember(key, b)
	}
	if seq == a.seq {
		if err != nil {
			a.state, a.msg = "error", err.Error()
			log.Printf("build %s: %v", key, err)
		} else {
			a.build, a.state, a.msg = b, "build", ""
		}
	}
	a.mu.Unlock()
	a.push()
}

type jgEntry struct {
	j         *core.Jungle
	at, tryAt time.Time
}

// jungleOf: a champion's first clears if loaded; asks push() to (re)load them when missing or over an hour old
// (the site collects all day). Call with a.mu held.
func (a *App) jungleOf(alias string) *core.Jungle {
	e := a.jgCache[alias]
	if e == nil || e.j == nil || time.Since(e.at) > time.Hour {
		a.needJg = append(a.needJg, alias)
	}
	if e == nil {
		return nil
	}
	return e.j
}

// fetchJungle reads a champion's common first clears (Riot match data on the build site).
func (a *App) fetchJungle(alias string) {
	j, err := core.FetchJungle(a.web, core.SiteURL, alias)
	if err != nil {
		log.Printf("jungle paths %s: %v", alias, err)
		return
	}
	a.mu.Lock()
	if a.jgCache == nil {
		a.jgCache = map[string]*jgEntry{}
	}
	e := a.jgCache[alias]
	if e == nil {
		e = &jgEntry{}
		a.jgCache[alias] = e
	}
	e.j, e.at = j, time.Now()
	a.mu.Unlock()
	a.push()
}

// sideView: the top routes of one side for the panel or card.
func sideView(j *core.Jungle, want string, n int) *jungleView {
	s, side := j.For(want)
	if s == nil || len(s.Paths) == 0 {
		return nil
	}
	if len(s.Paths) < n {
		n = len(s.Paths)
	}
	return &jungleView{Side: side, Mine: side == want || want == "", Games: s.Games, Patch: j.Patch, Paths: s.Paths[:n]}
}

// jungleView: your own first clears, when you're the jungler.
func (a *App) jungleView() *jungleView {
	if a.roleOf() != "jungle" || a.champ.Alias == "" {
		return nil
	}
	j := a.jungleOf(a.champ.Alias)
	if j == nil {
		return nil
	}
	return sideView(j, a.pick.Side, 2)
}

// enemyJungleView (champ select only, never in game): the enemy jungler's most common first clear on their side.
func (a *App) enemyJungleView(en []core.Champion, known []string) *jungleView {
	i, sure := core.EnemyIn("jungle", en, known, a.tier)
	if i < 0 {
		return nil
	}
	j := a.jungleOf(en[i].Alias)
	if j == nil {
		return nil
	}
	other := map[string]string{"blue": "red", "red": "blue"}[a.pick.Side]
	v := sideView(j, other, 1)
	if v == nil {
		return nil
	}
	v.Champ, v.ChampID, v.Sure = en[i].Name, en[i].Alias, sure
	return v
}

type enemyView struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Role     string         `json:"role"`
	Sure     bool           `json:"sure"`
	Ready    bool           `json:"ready"` // its build is loaded (Counters may still be empty: too few games)
	Counters []core.VsEntry `json:"counters,omitempty"`
}

type jungleView struct {
	Champ   string            `json:"champ,omitempty"` // the enemy jungler (enemy view only)
	ChampID string            `json:"champId,omitempty"`
	Sure    bool              `json:"sure,omitempty"` // that it's their jungler is clear, not just a likely guess
	Side    string            `json:"side"`
	Mine    bool              `json:"mine"` // false: your side has no data yet, this is the other side's
	Games   int               `json:"games"`
	Patch   string            `json:"patch"`
	Paths   []core.JunglePath `json:"paths"`
}

// loadTier keeps the site's tier list (role shares, for guessing enemy roles) for 3 hours.
func (a *App) loadTier() *core.TierList {
	a.mu.Lock()
	t, at := a.tier, a.tierAt
	a.mu.Unlock()
	if t != nil && time.Since(at) < 3*time.Hour {
		return t
	}
	nt, err := core.FetchTierList(a.web, core.SiteURL)
	if err != nil {
		log.Printf("tier list: %v", err)
		return t
	}
	a.mu.Lock()
	a.tier, a.tierAt = nt, time.Now()
	a.mu.Unlock()
	a.push()
	return nt
}

// loadClash reads the team's saved matches from the Clash helper and scouts one (the newest, or id).
// id "@client" shows the League client's scouting instead.
// loadClash reads a saved match from the Clash helper: the newest one when id is "".
func (a *App) loadClash(id string) {
	a.mu.Lock()
	if !a.clash {
		a.mu.Unlock()
		return
	}
	a.cl.seq++
	seq, gen := a.cl.seq, a.gen
	a.cl.Loading, a.cl.Err = true, ""
	a.mu.Unlock()
	a.push()
	finish := func(f func(c *clashState)) {
		a.mu.Lock()
		if seq == a.cl.seq && gen == a.gen && a.clash {
			a.cl.Loading = false
			f(&a.cl)
		}
		a.mu.Unlock()
		a.push()
	}
	list, err := core.ClashMatches(a.web, core.ClashURL)
	if err != nil {
		finish(func(c *clashState) { c.Err = err.Error() })
		return
	}
	if len(list) == 0 {
		finish(func(c *clashState) {
			c.Helper, c.Err = nil, "No scouting yet: the League client had none, and nothing is saved in the Clash helper."
		})
		return
	}
	if id == "" {
		id = list[0].ID
	}
	m, err := core.GetClashMatch(a.web, core.ClashURL, id)
	if err != nil {
		finish(func(c *clashState) { c.Err = err.Error() })
		return
	}
	sc := core.ScoutTeam(m, a.loadTier())
	finish(func(c *clashState) { c.Helper = &sc })
}

// loadClientClash reads this round's opponent from the League client's own Clash data (Clash games only).
// The client's raw answers go to the log, so the real formats can be checked after a Clash.
func (a *App) loadClientClash() {
	a.mu.Lock()
	lcu, clash, gen := a.lcu, a.clash, a.gen
	if lcu == nil || !clash { // never outside Clash
		a.mu.Unlock()
		return
	}
	a.cl.ClientLoading, a.cl.ClientErr = true, ""
	a.mu.Unlock()
	a.push()
	var champs map[int]core.Champion
	for i := 0; i < 20 && champs == nil; i++ { // the champion list arrives with the first session poll
		a.mu.Lock()
		champs = a.champs
		a.mu.Unlock()
		if champs == nil {
			time.Sleep(500 * time.Millisecond)
		}
	}
	logged := map[string]bool{}
	raw := func(path string, body []byte, err error) {
		if logged[path] {
			return
		}
		logged[path] = true
		if len(body) > 8000 {
			body = append(body[:8000], []byte("…")...)
		}
		log.Printf("clash raw %s err=%v: %s", path, err, body)
	}
	m, pos, err := lcu.ClashFromClient(lcu.CurrentPuuid(), champs, raw)
	var sc *core.Scout
	if err == nil {
		s := core.ScoutTeam(m, a.loadTier())
		sc = &s
		log.Printf("clash: scouted the opponent from the client (%d players)", len(s.Players))
	} else {
		log.Printf("clash: no scouting from the client: %v", err)
	}
	a.mu.Lock()
	if a.clash && gen == a.gen { // still the same champ select
		a.cl.ClientLoading = false
		if err != nil {
			a.cl.ClientErr = err.Error() // keep what an earlier read found
		} else {
			a.cl.Client, a.posOf = sc, pos
		}
	}
	a.mu.Unlock()
	a.forcePush()
}

// refreshClash reads both sources again (the panel's Refresh button; does nothing outside Clash).
func (a *App) refreshClash() {
	go a.loadClientClash()
	a.loadClash("")
}

// remember keeps a fetched build (caller holds a.mu). The cache is emptied when it gets big.
func (a *App) remember(k string, b *core.Build) {
	if len(a.cache) >= 150 {
		a.cache = map[string]cached{}
	}
	a.cache[k] = cached{b, time.Now()}
}

func (a *App) retry() {
	a.mu.Lock()
	a.key = ""
	p := a.pick
	a.mu.Unlock()
	a.onPick(p)
}

// ---- what the panel shows ----

type view struct {
	Version    string          `json:"version"`
	Test       bool            `json:"test"`
	TooLate    bool            `json:"tooLate"` // the game starts in a moment: Apply is locked
	State      string          `json:"state"`
	Msg        string          `json:"msg"`
	Mode       string          `json:"mode"`
	Queue      string          `json:"queue"`
	Name       string          `json:"name"`
	Alias      string          `json:"alias"`
	Role       string          `json:"role"`
	Locked     bool            `json:"locked"`
	Key        string          `json:"key"` // which build is on screen; clicks send it back
	Build      json.RawMessage `json:"build,omitempty"`
	Runes      []int           `json:"runeOrder"`
	Spells     []int           `json:"spellOrder"`
	Pref       core.Pref       `json:"remembered"`
	Applied    core.Pref       `json:"applied"`
	Matchup    *core.Matchup   `json:"matchup,omitempty"`
	Counter    []core.VsEntry  `json:"counters,omitempty"` // pick ideas against the lane opponent, while not locked in
	CntFor     string          `json:"countersFor,omitempty"`
	Bans       []banView       `json:"bans,omitempty"` // most banned (EUW Emerald+), until you lock in
	Clash      *clashView      `json:"clash,omitempty"`
	Level      int             `json:"level,omitempty"`      // in game: your level
	Next       int             `json:"next"`                 // in game: the skill order's next point (-1: none)
	Spend      bool            `json:"spend,omitempty"`      // in game: you have a skill point to spend now
	LineUp     bool            `json:"lineUp,omitempty"`     // in game: the skill marker is being lined up
	MarkerHint bool            `json:"markerHint,omitempty"` // in game: the skill marker isn't lined up yet
	Moving     bool            `json:"moving,omitempty"`     // in game: the card is being moved
	Hotkey     string          `json:"hotkey,omitempty"`
	Alpha      int             `json:"alpha,omitempty"`       // in game: the card's opacity in percent
	Size       int             `json:"size,omitempty"`        // in game: the card's size in percent
	Jungle     *jungleView     `json:"jungle,omitempty"`      // junglers: common first clears for your side
	EnemyJg    *jungleView     `json:"enemyJungle,omitempty"` // champ select only: the enemy jungler's usual first clear
	Enemies    []enemyView     `json:"enemies,omitempty"`     // champ select: enemy picks with counters on hover
	EarlyGame  bool            `json:"early,omitempty"`       // in game: before 5:00 (the card shows the first clear)
}

func (a *App) roleOf() string {
	if a.build != nil && a.build.Position != "" {
		return a.build.Position
	}
	return a.pick.Role
}

func (a *App) prefKey() string { return core.PrefKey(a.champ.Alias, a.roleOf()) }

func (a *App) view() view {
	if a.inGame {
		// the in-game card: the build you had in champ select and your own level. Nothing about other players.
		if !a.cardReady() {
			return view{State: "ingame-none", Version: version}
		}
		_, lined := a.skillSpot()
		next, spend := core.NextSkill(a.build.Skills.Order, core.Player{Level: a.level, Skills: a.skills}, a.champ.Alias)
		return view{State: "ingame", Version: version, Key: a.key, Name: a.champ.Name, Alias: a.champ.Alias, Role: a.roleOf(),
			Build: a.build.Raw, Level: a.level, Next: next, Spend: spend, LineUp: a.liningUp, MarkerHint: !lined && !a.cfg.MarkerOff, Moving: a.moving, Hotkey: cardHotkey, Alpha: a.cfg.cardAlpha(), Size: a.cfg.cardSize(),
			Jungle: a.jungleView(), EarlyGame: a.clockAt.IsZero() || a.clock+time.Since(a.clockAt).Seconds() < 300}
	}
	v := view{Key: a.key, Version: version, Test: a.cfg.TestMode, TooLate: a.timer.TooLate(a.timerAt, time.Now()), State: a.state, Msg: a.msg, Mode: a.mode, Queue: a.queue, Name: a.champ.Name, Alias: a.champ.Alias, Role: a.roleOf(), Locked: a.pick.Locked}
	if a.state == "pick" || a.state == "waiting" || a.state == "unsupported" {
		v.Name, v.Alias = "", ""
	}
	var en []core.Champion
	var known []string
	for _, e := range a.enemies {
		if ch, ok := a.champs[e.ChampionID]; ok {
			en = append(en, ch)
			known = append(known, a.posOf[e.Puuid])
		}
	}
	if a.build != nil && a.state == "build" {
		v.Build = a.build.Raw
		v.Pref = a.prefs.Get(a.prefKey())
		v.Runes, v.Spells = core.Order(a.build, v.Pref)
		v.Applied = a.applied[a.prefKey()]
		v.Jungle = a.jungleView()
		m := core.LaneMatchup(a.build, a.roleOf(), en, known, a.tier)
		v.Matchup = &m
		v.EnemyJg = a.enemyJungleView(en, known)
		if m.Opp != "" && m.OppID != "" && m.Role != "" && !a.pick.Locked {
			ck := m.OppID + ":" + m.Role
			if c, ok := a.cache[ck]; ok && time.Since(c.at) < 30*time.Minute {
				v.Counter, v.CntFor = core.Counters(c.b, 3), m.Opp
			} else {
				a.needCnt = ck
			}
		}
	}
	// the enemy picks, each with 5 counter-picks (from its own build's matchups) to show on hover; kept after you
	// lock in, so you can help your teammates pick (owner's ask, 2026-11-28)
	if a.state != "unsupported" && len(en) > 0 && len(en) <= 5 {
		roles, sure := core.EnemyRoles(en, known, a.tier)
		for i, e := range en {
			ev := enemyView{ID: e.Alias, Name: e.Name, Role: roles[i], Sure: sure[i]}
			if roles[i] != "" {
				ck := e.Alias + ":" + roles[i]
				if c, ok := a.cache[ck]; ok && time.Since(c.at) < 30*time.Minute {
					ev.Counters, ev.Ready = core.Counters(c.b, 5), true
				} else {
					a.needCnts = append(a.needCnts, ck)
				}
			}
			v.Enemies = append(v.Enemies, ev)
		}
	}
	// the most banned champions, to help with bans: shown until you lock in your own champion
	if a.state != "unsupported" && !a.pick.Locked {
		taken := map[string]bool{}
		for _, id := range a.banned {
			if ch, ok := a.champs[id]; ok {
				taken[ch.Alias] = true
			}
		}
		for _, b := range a.tier.TopBans(10) {
			v.Bans = append(v.Bans, banView{ID: b.ID, Name: b.Name, Ban: b.Ban, Taken: taken[b.ID]})
		}
	}
	if a.clash && a.state != "unsupported" {
		v.Clash = a.cl.view()
	}
	return v
}

func (a *App) push() {
	a.mu.Lock()
	v := a.view()
	if k := a.needCnt; k != "" {
		a.needCnts = append(a.needCnts, k)
	}
	for _, k := range a.needCnts {
		if a.cntNext == nil {
			a.cntNext = map[string]time.Time{}
		}
		if time.Now().After(a.cntNext[k]) {
			a.cntNext[k] = time.Now().Add(2 * time.Minute) // one try at a time; after a failure, wait before retrying
			go a.fetchCounters(k)
		}
	}
	a.needCnt, a.needCnts = "", nil
	for _, alias := range a.needJg {
		if a.jgCache == nil {
			a.jgCache = map[string]*jgEntry{}
		}
		e := a.jgCache[alias]
		if e == nil {
			e = &jgEntry{}
			a.jgCache[alias] = e
		}
		if time.Since(e.tryAt) > 3*time.Minute { // one try at a time; after a failure, wait before retrying
			e.tryAt = time.Now()
			go a.fetchJungle(alias)
		}
	}
	a.needJg = nil
	w := a.w
	b, _ := json.Marshal(v)
	s := string(b)
	if s == a.lastSent || w == nil {
		a.mu.Unlock()
		return
	}
	a.lastSent = s
	a.mu.Unlock()
	w.Dispatch(func() { w.Eval("window.render && window.render(" + s + ")") })
}

// fetchCounters reads the lane opponent's build (key "Alias:role"); its matchups give the counter-pick ideas.
func (a *App) fetchCounters(k string) {
	alias, role, _ := strings.Cut(k, ":")
	b, err := core.FetchBuild(a.web, core.SiteURL, alias, role)
	a.mu.Lock()
	if err == nil {
		a.remember(k, b)
	} else {
		log.Printf("counters %s: %v", k, err)
	}
	a.mu.Unlock()
	if err == nil {
		a.push()
	}
}

func (a *App) forcePush() {
	a.mu.Lock()
	a.lastSent = ""
	a.mu.Unlock()
	a.push()
}

// ---- clicks in the panel ----

// stale: the panel showed another champion's build than the one now picked (hovers change fast)
const stale = "You just changed champion. Check the panel and click again."
const tooLate = "Too late to change safely: the game starts in a moment."

func (a *App) applyRunes(shown string, i, replaceID int) core.Result {
	a.mu.Lock()
	lcu, b, test, cur, own := a.lcu, a.build, a.cfg.TestMode, a.key, append([]int{}, a.cfg.OwnPages...)
	late := a.timer.TooLate(a.timerAt, time.Now())
	key, name := a.prefKey(), core.PageName(a.champ.Name, a.roleOf())
	a.mu.Unlock()
	if lcu == nil {
		return core.Result{Msg: "Not connected to League right now."}
	}
	if late {
		return core.Result{Msg: tooLate}
	}
	if b == nil || shown != cur || i < 0 || i >= len(b.Runes) {
		return core.Result{Msg: stale}
	}
	r := lcu.ApplyRunes(b.Runes[i], name, own, replaceID, test)
	log.Printf("runes %s #%d: ok=%v %s", key, i, r.OK, r.Msg)
	if !test && r.PageID != 0 { // also when the check afterwards failed: the page the app wrote is still its own
		a.rememberPage(r.PageID)
	}
	if r.OK && !test {
		ks, sub := b.Runes[i].Keystone(), b.Runes[i].Secondary.ID
		a.prefs.Set(key, func(p *core.Pref) { p.Keystone, p.Sub = ks, sub })
		a.mu.Lock()
		ap := a.applied[key]
		ap.Keystone, ap.Sub = ks, sub
		a.applied[key] = ap
		a.mu.Unlock()
		go a.forcePush()
	}
	return r
}

// rememberPage keeps the id of the rune page the app wrote, newest first, at most 10 (one per account used on this PC).
func (a *App) rememberPage(id int) {
	a.mu.Lock()
	list := []int{id}
	for _, x := range a.cfg.OwnPages {
		if x != id && len(list) < 10 {
			list = append(list, x)
		}
	}
	a.cfg.OwnPages = list
	a.mu.Unlock()
	a.saveConfig()
}

func (a *App) applySpells(shown string, i int) core.Result {
	a.mu.Lock()
	lcu, b, test, pick, cur := a.lcu, a.build, a.cfg.TestMode, a.pick, a.key
	key := a.prefKey()
	late := a.timer.TooLate(a.timerAt, time.Now())
	a.mu.Unlock()
	if lcu == nil {
		return core.Result{Msg: "Not connected to League right now."}
	}
	if late {
		return core.Result{Msg: tooLate}
	}
	if b == nil || shown != cur || i < 0 || i >= len(b.Summs) {
		return core.Result{Msg: stale}
	}
	pair := b.Summs[i].IDs
	names := map[int]string{}
	for k, sp := range b.Summs[i].Spells {
		if k < len(pair) {
			names[pair[k]] = sp.Name
		}
	}
	r := lcu.ApplySpells(pair, names, pick.Spell1, pick.Spell2, test)
	log.Printf("spells %s %v: ok=%v %s", key, pair, r.OK, r.Msg)
	if r.OK && !test {
		a.prefs.Set(key, func(p *core.Pref) { p.Spells = append([]int{}, pair...) })
		a.mu.Lock()
		ap := a.applied[key]
		ap.Spells = append([]int{}, pair...)
		a.applied[key] = ap
		a.mu.Unlock()
		go a.forcePush()
	}
	return r
}

func (a *App) openBuild() {
	a.mu.Lock()
	u := core.SiteURL + "/"
	if a.champ.Name != "" {
		u += "?c=" + url.QueryEscape(a.champ.Name)
		if r := a.roleOf(); r != "" {
			u += "&r=" + r
		}
	}
	a.mu.Unlock()
	openURL(u)
}

// ---- the panel window ----

func (a *App) uiLoop() {
	<-a.uiWanted
	if _, err := webviewloader.GetInstalledVersion(); err != nil {
		log.Printf("WebView2 missing: %v", err)
		a.setStatus("Panel needs Microsoft Edge WebView2")
		messageBox("The panel needs Microsoft Edge WebView2, which comes with Windows 10 and 11 but is missing here.\n\nInstall it from Microsoft (the “Evergreen Bootstrapper”), then restart Champ Select Companion.", mbIconWarning)
		openURL("https://developer.microsoft.com/microsoft-edge/webview2/")
		select {}
	}
	// the web view's own background is transparent, so the in-game card can be see-through; every page in it
	// paints its own background (the panel's is opaque), so nothing else changes
	_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "0")
	w := webview.NewWithOptions(webview.WebViewOptions{
		DataPath:      filepath.Join(a.dir, "webview"),
		WindowOptions: webview.WindowOptions{Title: "Champ select", Width: panelW, Height: panelH},
	})
	if w == nil {
		log.Printf("could not create the panel")
		a.mu.Lock()
		a.panelErr = "Windows couldn't create the panel window"
		a.mu.Unlock()
		a.setStatus("Panel failed to start")
		select {}
	}
	h := uintptr(w.Window())
	makePanel(h)
	makeMarker() // on this thread: the panel's message loop serves the marker's windows too
	_ = w.Bind("ready", func() { go a.forcePush() })
	_ = w.Bind("applyRunes", func(key string, i, replaceID int) core.Result { return a.applyRunes(key, i, replaceID) })
	_ = w.Bind("applySpells", func(key string, i int) core.Result { return a.applySpells(key, i) })
	_ = w.Bind("drag", func() { startDrag(h) })
	_ = w.Bind("hidePanel", func() {
		a.mu.Lock()
		a.hidden = true
		a.mu.Unlock()
		hideWindow(h)
	})
	_ = w.Bind("openBuild", func() { a.openBuild() })
	_ = w.Bind("retry", func() { go a.retry() })
	_ = w.Bind("clashRefresh", func() { go a.refreshClash() })
	_ = w.Bind("openClash", func() { openURL(core.ClashURL + "/") })
	_ = w.Bind("cardDone", func() { go a.setMoving(false) })
	_ = w.Bind("cardHeight", func(px int, dpr float64) {
		if px < 40 || px > 800 {
			return
		}
		a.mu.Lock()
		a.cardH = int32(px)
		if dpr >= 0.5 && dpr <= 5 {
			a.cardDPR = dpr
		}
		a.mu.Unlock()
	})
	w.SetHtml(panelPage())
	a.mu.Lock()
	a.w, a.hwnd = w, h
	a.mu.Unlock()
	go a.follow(h)
	go a.markerLoop()
	go a.objectiveLoop()
	w.Run()
	// Run only returns if the window was closed (Alt+F4): start a fresh copy rather than run without a panel.
	log.Printf("panel window closed; restarting")
	relaunch("--restart")
	os.Exit(0)
}

// follow keeps the panel beside the client while champ select is on and the client (or the panel) is in front.
func (a *App) follow(h uintptr) {
	var last, seen, lastCard rect
	shown, styled, styledMoving, styledSee := false, false, false, false
	hoverAt := [2]int32{-1, -1} // the pointer over the card, in page pixels (-1 = not over it)
	for {
		// ---- in game: the build card over the game ----
		a.mu.Lock()
		card := a.inGame && a.cardReady() && !a.cardHidden
		moving := a.moving
		see := a.cfg.cardAlpha() < 100
		dx, dy, ch, size, dpr := a.cfg.CardDX, a.cfg.CardDY, a.cardH, a.cfg.cardSize(), a.cardDPR
		a.mu.Unlock()
		if card {
			g, fg := gameWindow(), foreground()
			// while moving, show it even if the tray took the focus (else "Move" would seem to do nothing)
			if g != 0 && !isIconic(g) && isVisible(g) && (fg == g || fg == h || moving) {
				if !styled || styledMoving != moving || styledSee != see {
					setCardStyle(h, true, !moving, see)
					styled, styledMoving, styledSee = true, moving, see
				}
				// the card's own pixel scale, as the page reports it: not the game's (League says 100 % even when
				// Windows is at 125 %, which made the card too small and the hover land off by that factor)
				sc := dpr
				if sc <= 0 {
					sc = dpiScale(h)
				}
				if gr, ok := windowRect(g); ok {
					r := cardRect(gr, sc, dx, dy, ch, size)
					switch {
					case !moving && (r != lastCard || !shown):
						placeTopmost(h, r, true)
						lastCard = r
					case moving && lastCard.Bottom-lastCard.Top != r.Bottom-r.Top: // new height while moving: keep the spot
						if cur, ok := windowRect(h); ok {
							r = rect{Left: cur.Left, Top: cur.Top, Right: cur.Left + r.Right - r.Left, Bottom: cur.Top + r.Bottom - r.Top}
						}
						placeTopmost(h, r, true)
						lastCard = r
					case !shown:
						placeTopmost(h, r, true)
					}
					shown = true
					// hover names: the card lets clicks through, so it never sees the mouse; the app reads where the
					// pointer is and tells the page, which shows the name of what's under it
					at := [2]int32{-1, -1}
					if x, y, ok := cursorPos(); ok && !moving {
						if cr, ok := windowRect(h); ok && x >= cr.Left && x < cr.Right && y >= cr.Top && y < cr.Bottom {
							at = core.CardPointer(cr.Left, cr.Top, x, y, sc)
						}
					}
					if at != hoverAt {
						hoverAt = at
						a.mu.Lock()
						w := a.w
						a.mu.Unlock()
						if w != nil {
							js := fmt.Sprintf("window.hover && window.hover(%d,%d)", at[0], at[1])
							w.Dispatch(func() { w.Eval(js) })
						}
					}
				}
			} else if shown {
				hideWindow(h)
				shown = false
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if styled { // back from the card: the normal panel again
			hideWindow(h)
			setCardStyle(h, false, false, false)
			styled, shown, lastCard, last = false, false, rect{}, rect{}
		}
		if pr, ok := windowRect(h); ok && shown && pr != seen {
			if seen != (rect{}) {
				go a.forcePush()
			}
			seen = pr
		}
		a.mu.Lock()
		want := a.inSelect && !a.inGame && a.cfg.ShowPanel && !a.hidden && core.SupportedMode(a.mode) // no panel in ARAM, Arena, ...
		dragged := a.dragged
		a.mu.Unlock()
		// the player moved the panel: leave it where they put it for the rest of this champ select
		if shown && !dragged && last != (rect{}) {
			if pr, ok := windowRect(h); ok && pr != last {
				a.mu.Lock()
				a.dragged, dragged = true, true
				a.mu.Unlock()
			}
		}
		c := clientWindow()
		fg := foreground()
		if want && c != 0 && !isIconic(c) && isVisible(c) && (fg == c || fg == h) {
			cr, ok := windowRect(c)
			if ok {
				r := panelRect(cr, workArea(c), dpiScale(c))
				move := !dragged && r != last
				if move || !shown {
					placeTopmost(h, r, move || (!dragged && !shown))
					if move {
						last = r
					}
				}
				shown = true
			}
		} else if shown {
			hideWindow(h)
			shown = false
			if !want {
				last = rect{}
			}
		}
		if c == 0 {
			time.Sleep(time.Second)
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *App) showAgain() {
	a.mu.Lock()
	a.hidden, a.dragged = false, false
	a.cfg.ShowPanel = true
	a.mu.Unlock()
	a.saveConfig()
	a.uiOnce.Do(func() { a.uiWanted <- struct{}{} })
}

func (a *App) setShowPanel(on bool) {
	a.mu.Lock()
	a.cfg.ShowPanel = on
	in := a.inSelect
	a.mu.Unlock()
	a.saveConfig()
	if on && in {
		a.uiOnce.Do(func() { a.uiWanted <- struct{}{} })
	}
}

func (a *App) setTestMode(on bool) {
	a.mu.Lock()
	a.cfg.TestMode = on
	a.mu.Unlock()
	a.saveConfig()
	a.forcePush()
}

// ---- small helpers ----

func readAllLimit(r io.Reader, n int64) ([]byte, error) { return io.ReadAll(io.LimitReader(r, n)) }

func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }
