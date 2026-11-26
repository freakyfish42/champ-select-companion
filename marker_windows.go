package main

// The skill marker: a pulsing frame over the game's own Q/W/E/R icon for the next skill point in the build's
// order, while you have a point to spend. Two tiny native windows (no web view): they never take focus, every
// click goes through them to the game, and they send nothing to the game. Only while lining up ("Line up the
// skill marker" in the tray) do they take the mouse, as a blue box (Q) and a red box (R) you drag over your icons.

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/freakyfish42/champ-select-companion/core"
	"golang.org/x/sys/windows"
)

var (
	pRegisterClassExW   = user32.NewProc("RegisterClassExW")
	pCreateWindowExW    = user32.NewProc("CreateWindowExW")
	pDefWindowProcW     = user32.NewProc("DefWindowProcW")
	pFillRect           = user32.NewProc("FillRect")
	pInvalidateRect     = user32.NewProc("InvalidateRect")
	pSetWindowRgn       = user32.NewProc("SetWindowRgn")
	pLoadCursorW        = user32.NewProc("LoadCursorW")
	pCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	pCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	pCombineRgn         = gdi32.NewProc("CombineRgn")
)

const (
	wmEraseBkgnd    = 0x0014
	wmMouseActivate = 0x0021
	wmNCHitTest     = 0x0084
	maNoActivate    = 3
	rgnDiff         = 4
)

// the abilities' colours, as on the card (Windows wants 0x00BBGGRR)
var skillColours = [4]uint32{0x00FFC49F, 0x00B0E69E, 0x008AD0F0, 0x009F9FFF}

type markerWins struct {
	mu      sync.Mutex
	win     [2]uintptr // [0] the marker (and the Q box while lining up), [1] the R box while lining up
	brush   [4]uintptr
	paint   map[uintptr]uintptr // window → brush it's filled with
	lining  bool
	shapeOf [2]string // what each window was last shaped as, so it isn't redone 25 times a second
}

var marker = &markerWins{paint: map[uintptr]uintptr{}}

func markerProc(h, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		marker.mu.Lock()
		b := marker.paint[h]
		marker.mu.Unlock()
		if b != 0 {
			var r rect
			pGetClientRectW.Call(h, uintptr(unsafe.Pointer(&r)))
			pFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), b)
			return 1
		}
	case wmMouseActivate:
		return maNoActivate
	case wmNCHitTest:
		marker.mu.Lock()
		lining := marker.lining
		marker.mu.Unlock()
		if lining {
			return htCaption // drag the box anywhere on it
		}
	}
	r, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
	return r
}

// makeMarker creates the two windows, hidden. Call it on the panel's UI thread (its message loop serves them).
func makeMarker() {
	inst, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	cls, _ := windows.UTF16PtrFromString("ChampSelectCompanionMarker")
	cursor, _, _ := pLoadCursorW.Call(0, 32646) // IDC_SIZEALL: shows while lining up
	wc := struct {
		Size, Style                     uint32
		WndProc                         uintptr
		ClsExtra, WndExtra              int32
		Instance, Icon, Cursor, BgBrush uintptr
		MenuName, ClassName             *uint16
		IconSm                          uintptr
	}{Style: 0, WndProc: windows.NewCallback(markerProc), Instance: inst, Cursor: cursor, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Printf("skill marker: register class: %v", err)
		return
	}
	for i, c := range skillColours {
		marker.brush[i], _, _ = pCreateSolidBrush.Call(uintptr(c))
	}
	for i := range marker.win {
		h, _, err := pCreateWindowExW.Call(markerEx(false), uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)),
			wsPopup, 0, 0, 40, 40, 0, 0, inst, 0)
		if h == 0 {
			log.Printf("skill marker: create window: %v", err)
			return
		}
		marker.win[i] = h
	}
}

// markerEx: never takes focus, never in the taskbar, and lets every click through except while lining up.
func markerEx(lining bool) uintptr {
	ex := uintptr(wsExToolWindow | wsExLayered | wsExNoActivate | wsExTopmost)
	if !lining {
		ex |= wsExTransparent
	}
	return ex
}

func (m *markerWins) ready() bool { return m.win[0] != 0 && m.win[1] != 0 }

// shape makes window i a ring (the marker) or a filled box (lining up), sized for r, in colour slot.
func (m *markerWins) shape(i int, r rect, slot int, ring bool) {
	w, h := r.Right-r.Left, r.Bottom-r.Top
	key := fmt.Sprint(ring, slot, w, h)
	h0 := m.win[i]
	m.mu.Lock()
	same := m.shapeOf[i] == key
	m.shapeOf[i] = key
	m.paint[h0] = m.brush[slot]
	m.mu.Unlock()
	if same {
		return
	}
	round := uintptr(max32(6, w/6))
	outer, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), round, round)
	if ring {
		t := max32(3, w/14) // the frame's thickness
		inner, _, _ := pCreateRoundRectRgn.Call(uintptr(t), uintptr(t), uintptr(w-t+1), uintptr(h-t+1), round, round)
		pCombineRgn.Call(outer, outer, inner, rgnDiff)
		pDeleteObject.Call(inner)
	}
	pSetWindowRgn.Call(h0, outer, 1) // Windows owns the region from here
	pInvalidateRect.Call(h0, 0, 1)
}

func (m *markerWins) setLining(on bool) {
	m.mu.Lock()
	m.lining = on
	m.shapeOf = [2]string{}
	m.mu.Unlock()
	for _, h := range m.win {
		pSetWindowLongPtrW.Call(h, gwlExStyle, markerEx(on))
		pSetWindowPos.Call(h, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged)
	}
}

func (m *markerWins) hide() {
	for _, h := range m.win {
		if h != 0 && isVisible(h) {
			hideWindow(h)
		}
	}
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func skillBox(game rect, s core.SkillSpot, slot int) rect {
	return rect(core.SkillBox(core.Rect(game), s, slot))
}

// markerLoop shows the marker over the right icon while you have a point to spend (and the card shows), and
// the two boxes while lining up.
func (a *App) markerLoop() {
	start := time.Now()
	lined := false // the boxes for lining up are out (placed once, then the player moves them)
	var hudAt time.Time
	for {
		if !marker.ready() {
			time.Sleep(time.Second)
			continue
		}
		// League's HUD Scale (it can change mid-game in the options), every few seconds while a game runs
		a.mu.Lock()
		in := a.inGame
		a.mu.Unlock()
		if in && time.Since(hudAt) > 5*time.Second {
			s, ok := hudScale()
			a.mu.Lock()
			a.hud, a.hudOK = s, ok
			a.mu.Unlock()
			hudAt = time.Now()
		}
		a.mu.Lock()
		card := a.inGame && a.cardReady() && !a.cardHidden
		lining := a.liningUp && a.inGame
		spot, have := a.skillSpot()
		slot := -1
		if card && have && !a.cfg.MarkerOff && a.build != nil {
			ord := a.build.Skills.Order
			if i, spend := core.NextSkill(ord, core.Player{Level: a.level, Skills: a.skills}, a.champ.Alias); spend && i >= 0 {
				slot = core.SkillSlot(ord[i])
			}
		}
		panel := a.hwnd
		a.mu.Unlock()

		g, fg := gameWindow(), foreground()
		gr, ok := clientRect(g)
		up := g != 0 && ok && !isIconic(g) && isVisible(g) && (fg == g || fg == panel || lining)
		switch {
		case lining && up:
			if !lined {
				marker.setLining(true)
				for i, s := range []int{0, 3} {
					r := skillBox(gr, spot, s)
					marker.shape(i, r, s, false)
					pSetLayeredAttrs.Call(marker.win[i], 0, 200, lwaAlpha)
					placeTopmost(marker.win[i], r, true)
				}
				lined = true
			}
			time.Sleep(100 * time.Millisecond)
			continue
		case slot >= 0 && up:
			if lined {
				marker.setLining(false)
				lined = false
			}
			r := skillBox(gr, spot, slot)
			marker.shape(0, r, slot, true)
			// pulse: fades between half and full, about once a second
			ph := math.Sin(2 * math.Pi * time.Since(start).Seconds() / 1.1)
			pSetLayeredAttrs.Call(marker.win[0], 0, uintptr(150+105*(0.5+0.5*ph)), lwaAlpha)
			if cur, ok := windowRect(marker.win[0]); !ok || cur != r || !isVisible(marker.win[0]) {
				placeTopmost(marker.win[0], r, true)
			}
			if isVisible(marker.win[1]) {
				hideWindow(marker.win[1])
			}
			time.Sleep(40 * time.Millisecond)
			continue
		}
		if lined && !lining {
			marker.setLining(false)
			lined = false
		}
		marker.hide()
		time.Sleep(200 * time.Millisecond)
	}
}

// setLiningUp starts or ends lining up the skill marker. Ending it saves where the boxes are; it returns
// whether lining up is on afterwards.
func (a *App) setLiningUp(on bool) bool {
	a.mu.Lock()
	in := a.inGame && a.cardReady()
	a.mu.Unlock()
	if on {
		if !in || !marker.ready() {
			messageBox("The skill marker is lined up during a game (the Practice Tool works): start one, then choose “Line up the skill marker” again.", mbIconInfo)
			return false
		}
		a.mu.Lock()
		a.liningUp, a.cardHidden = true, false
		a.mu.Unlock()
		go a.forcePush()
		return true
	}
	a.mu.Lock()
	was := a.liningUp
	a.mu.Unlock()
	if !was {
		return false
	}
	g := gameWindow()
	gr, ok1 := clientRect(g)
	q, ok2 := windowRect(marker.win[0])
	r, ok3 := windowRect(marker.win[1])
	if g != 0 && ok1 && ok2 && ok3 {
		s, err := core.SpotFrom(core.Rect(gr), core.Rect(q), core.Rect(r))
		if err != nil {
			messageBox("Not saved: "+err.Error()+".", mbIconWarning)
			return true // still lining up
		}
		a.mu.Lock()
		if a.hudOK {
			h := a.hud
			s.HUD = &h
		}
		a.cfg.SkillSpot = &s
		a.cfg.MarkerOff = false
		a.mu.Unlock()
		a.saveConfig()
		log.Printf("skill marker lined up: Q %.4f,%.4f R %.4f,%.4f (game %dx%d, HUD scale %v)", s.QX, s.QY, s.RX, s.RY, gr.Right-gr.Left, gr.Bottom-gr.Top, hudText(s.HUD))
	}
	a.mu.Lock()
	a.liningUp = false
	a.mu.Unlock()
	go a.forcePush()
	return false
}

func (a *App) setMarkerOff(off bool) {
	a.mu.Lock()
	a.cfg.MarkerOff = off
	a.mu.Unlock()
	a.saveConfig()
	go a.forcePush()
}

func (a *App) liningUpNow() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.liningUp
}

func hudText(p *float64) string {
	if p == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.0f", *p*100)
}

// skillSpot: where the ability bar is (see core.ChooseSpot). Call with a.mu held.
func (a *App) skillSpot() (core.SkillSpot, bool) {
	return core.ChooseSpot(a.cfg.SkillSpot, a.hud, a.hudOK)
}
