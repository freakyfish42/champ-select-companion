package main

import (
	"fmt"
	"os"
	"runtime"

	"fyne.io/systray"
	"github.com/freakyfish42/champ-select-companion/core"
)

// runTray runs the tray icon and its menu on a thread of its own (the panel owns the main thread).
func runTray(a *App) {
	runtime.LockOSThread()
	systray.Run(func() { trayReady(a) }, func() {})
}

func trayReady(a *App) {
	systray.SetIcon(iconICO)
	systray.SetTooltip("Champ Select Companion")
	title := systray.AddMenuItem("Champ Select Companion "+version, "")
	title.Disable()
	status := systray.AddMenuItem("Waiting for League", "")
	status.Disable()
	a.mu.Lock()
	a.onStatus = func(s string) { status.SetTitle(s) }
	cur := a.status
	cfg := a.cfg
	a.mu.Unlock()
	if cur != "" {
		status.SetTitle(cur)
	}
	systray.AddSeparator()
	showAgain := systray.AddMenuItem("Show the panel again", "Bring back the panel if you closed it in this champ select")
	showPanel := systray.AddMenuItemCheckbox("Show panel in champ select", "", cfg.ShowPanel)
	showCard := systray.AddMenuItemCheckbox("Show build card in game", "Your build and skill order over the game (League in Borderless or Windowed). "+cardHotkey+" hides it", !cfg.HideCard)
	moveCard := systray.AddMenuItem("Move the in-game card", "While a game runs: drag the card where you want it, then click Done")
	markerOn := systray.AddMenuItemCheckbox("Skill marker on the ability bar", "A pulsing frame on your Q/W/E/R icon when it's time to level it (from the build's skill order)", !cfg.MarkerOff)
	lineUp := systray.AddMenuItem("Line up the skill marker", "While a game runs: drag the blue box onto your Q icon and the red box onto R, then click here again")
	alphaMenu := systray.AddMenuItem("In-game card opacity", "How much of the game shows through the card")
	alphas := map[int]*systray.MenuItem{}
	for _, pct := range []int{100, 80, 60} {
		alphas[pct] = alphaMenu.AddSubMenuItemCheckbox(fmt.Sprintf("%d %%", pct), "", cfg.cardAlpha() == pct)
	}
	sizeMenu := systray.AddMenuItem("In-game card size", "How big the card is over the game")
	sizes := map[int]*systray.MenuItem{}
	for _, pct := range []int{100, 125, 150} {
		sizes[pct] = sizeMenu.AddSubMenuItemCheckbox(fmt.Sprintf("%d %%", pct), "", cfg.cardSize() == pct)
	}
	pickSize := func(pct int) {
		for p, m := range sizes {
			if p == pct {
				m.Check()
			} else {
				m.Uncheck()
			}
		}
		a.setCardSize(pct)
	}
	pickAlpha := func(pct int) {
		for p, m := range alphas {
			if p == pct {
				m.Check()
			} else {
				m.Uncheck()
			}
		}
		a.setCardAlpha(pct)
	}
	testMode := systray.AddMenuItemCheckbox("Test mode (Apply only describes)", "Apply buttons say what they would change, and change nothing", cfg.TestMode)
	autostart := systray.AddMenuItemCheckbox("Start with Windows", "", autostartEnabled())
	systray.AddSeparator()
	site := systray.AddMenuItem("Open the build site", "")
	check := systray.AddMenuItem("Check setup", "Tests the connection to League, the build site, the Clash helper, the panel and updates")
	logs := systray.AddMenuItem("Open log folder", "")
	update := systray.AddMenuItem("Check for updates", "")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "")

	toggle := func(m *systray.MenuItem) bool {
		if m.Checked() {
			m.Uncheck()
			return false
		}
		m.Check()
		return true
	}
	for {
		select {
		case <-showAgain.ClickedCh:
			showPanel.Check()
			a.showAgain()
		case <-showPanel.ClickedCh:
			a.setShowPanel(toggle(showPanel))
		case <-showCard.ClickedCh:
			a.setShowCard(toggle(showCard))
		case <-moveCard.ClickedCh:
			go a.setMoving(true)
		case <-markerOn.ClickedCh:
			a.setMarkerOff(!toggle(markerOn))
		case <-lineUp.ClickedCh:
			lineUp.Disable()
			go func() {
				on := !a.liningUpNow()
				if a.setLiningUp(on) {
					lineUp.SetTitle("Done lining up the skill marker")
					markerOn.Check()
				} else {
					lineUp.SetTitle("Line up the skill marker")
				}
				lineUp.Enable()
			}()
		case <-alphas[100].ClickedCh:
			pickAlpha(100)
		case <-alphas[80].ClickedCh:
			pickAlpha(80)
		case <-alphas[60].ClickedCh:
			pickAlpha(60)
		case <-sizes[100].ClickedCh:
			pickSize(100)
		case <-sizes[125].ClickedCh:
			pickSize(125)
		case <-sizes[150].ClickedCh:
			pickSize(150)
		case <-testMode.ClickedCh:
			a.setTestMode(toggle(testMode))
		case <-autostart.ClickedCh:
			on := toggle(autostart)
			if exe, err := os.Executable(); err == nil {
				if err := setAutostart(on, exe); err != nil {
					messageBox("Couldn't change the start-with-Windows setting: "+err.Error(), mbIconWarning)
				}
			}
		case <-site.ClickedCh:
			openURL(core.SiteURL + "/")
		case <-check.ClickedCh:
			// greyed out with a "Checking" title while it runs, so a second click can't go unnoticed
			check.SetTitle("Checking setup…")
			check.Disable()
			go func() {
				a.checkSetup()
				check.SetTitle("Check setup")
				check.Enable()
			}()
		case <-logs.ClickedCh:
			openURL(a.dir)
		case <-update.ClickedCh:
			go a.checkUpdate(true)
		case <-quit.ClickedCh:
			systray.Quit()
			os.Exit(0)
		}
	}
}
