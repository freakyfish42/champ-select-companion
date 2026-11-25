package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/freakyfish42/lomsland-companion/core"
	"golang.org/x/sys/windows"
)

const (
	exeName   = "lomsland-companion.exe"
	latestAPI = "https://api.github.com/repos/freakyfish42/lomsland-companion/releases/latest"
)

func installedExe(dir string) string { return filepath.Join(dir, exeName) }

func isInstalledCopy(dir string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(exe), filepath.Clean(installedExe(dir)))
}

// install copies the downloaded exe to %LOCALAPPDATA%\lomsland-companion, turns on start with Windows and starts it.
// Copying the bytes (not the file) also leaves Windows' "downloaded from the internet" mark behind.
func install(dir string) {
	m, ok := singleInstance()
	if !ok {
		messageBox("lømsland companion is already running. Look for its icon in the tray, next to the clock.", mbIconInfo)
		return
	}
	release := sync.OnceFunc(func() { windows.CloseHandle(m) })
	defer release()
	self, err := os.Executable()
	if err == nil {
		err = copyFile(self, installedExe(dir))
	}
	if err != nil {
		messageBox("Couldn't install lømsland companion: "+err.Error(), mbIconWarning)
		return
	}
	_ = setAutostart(true, installedExe(dir))
	release() // the installed copy takes over the single-instance lock
	log.Printf("installed %s to %s", version, dir)
	if err := start(installedExe(dir), "--installed"); err != nil {
		messageBox("Installed, but couldn't start it: "+err.Error(), mbIconWarning)
		return
	}
	messageBox("lømsland companion is installed.\n\nIt lives in the tray next to the clock (you may need to click ^ to see it) and starts with Windows. "+
		"When you're in champ select, a panel appears beside the League client with rune pages and summoner spells. Nothing changes in your client until you click Apply.\n\n"+
		"You can delete the file you just downloaded.", mbIconInfo)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func start(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func relaunch(args ...string) {
	if exe, err := os.Executable(); err == nil {
		_ = start(exe, args...)
	}
}

func cleanupOld(dir string) {
	_ = os.Remove(filepath.Join(dir, "lomsland-companion.old.exe"))
	_ = os.Remove(filepath.Join(dir, "lomsland-companion.new.exe"))
}

// ---- updates from GitHub releases ----

func (a *App) updateLoop() {
	time.Sleep(time.Minute)
	for {
		a.checkUpdate(false)
		time.Sleep(6 * time.Hour)
	}
}

func (a *App) busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch a.phase {
	case "ChampSelect", "InProgress", "GameStart", "Reconnect", "ReadyCheck":
		return true
	}
	return a.inSelect
}

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var updating sync.Mutex

func (a *App) checkUpdate(manual bool) {
	if !updating.TryLock() {
		return // a check is already running
	}
	defer updating.Unlock()
	say := func(s string) {
		log.Printf("update: %s", s)
		if manual {
			messageBox(s, mbIconInfo)
		}
	}
	if version == "dev" {
		say("This is a development build; it doesn't update itself.")
		return
	}
	if a.busy() && !manual {
		return // try again later, never mid-game
	}
	req, _ := http.NewRequest("GET", latestAPI, nil)
	req.Header.Set("User-Agent", "lomsland-companion/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := a.web.Do(req)
	if err != nil {
		say("Couldn't check for updates: " + err.Error())
		return
	}
	var rel release
	err = json.NewDecoder(res.Body).Decode(&rel)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		say("Couldn't check for updates (GitHub answered " + res.Status + ").")
		return
	}
	if !core.Newer(version, rel.Tag) {
		say("You have the latest version (" + version + ").")
		return
	}
	if updateKey == "" {
		say("Version " + rel.Tag + " is out. This copy has no update key, so download it from github.com/freakyfish42/lomsland-companion/releases.")
		return
	}
	var exeURL, sigURL string
	for _, as := range rel.Assets {
		switch as.Name {
		case exeName:
			exeURL = as.URL
		case exeName + ".sig":
			sigURL = as.URL
		}
	}
	if exeURL == "" || sigURL == "" {
		say("Version " + rel.Tag + " is out, but its download or signature is missing.")
		return
	}
	bin, err := a.download(exeURL, 64<<20)
	if err != nil {
		say("Couldn't download " + rel.Tag + ": " + err.Error())
		return
	}
	sig, err := a.download(sigURL, 1<<10)
	if err != nil {
		say("Couldn't download " + rel.Tag + ": " + err.Error())
		return
	}
	// only install what the owner signed, for exactly this version
	if err := core.VerifyRelease(updateKey, rel.Tag, bin, sig); err != nil {
		say("Version " + rel.Tag + " was NOT installed: " + err.Error() + ".")
		return
	}
	if a.busy() {
		return // a game started while downloading
	}
	exe := installedExe(a.dir)
	newPath, oldPath := filepath.Join(a.dir, "lomsland-companion.new.exe"), filepath.Join(a.dir, "lomsland-companion.old.exe")
	if err := os.WriteFile(newPath, bin, 0o755); err != nil {
		say("Couldn't save " + rel.Tag + ": " + err.Error())
		return
	}
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil { // a running exe can be renamed, not overwritten
		say("Couldn't install " + rel.Tag + ": " + err.Error())
		return
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		say("Couldn't install " + rel.Tag + ": " + err.Error())
		return
	}
	log.Printf("updated to %s, restarting", rel.Tag)
	if err := start(exe, "--updated"); err != nil {
		_ = os.Rename(exe, newPath)
		_ = os.Rename(oldPath, exe)
		say("Couldn't start " + rel.Tag + ": " + err.Error())
		return
	}
	os.Exit(0)
}

func (a *App) download(u string, max int64) ([]byte, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "lomsland-companion/"+version)
	hc := &http.Client{Timeout: 2 * time.Minute}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errorf("GitHub answered %s", res.Status)
	}
	return readAllLimit(res.Body, max)
}
