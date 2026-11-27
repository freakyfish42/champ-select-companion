package main

// The objective countdown: a small box at the top middle of the game, from a minute before each neutral
// objective's fixed first spawn (core.Objectives) until a few seconds after. It follows the game clock only.
// Like the card and the skill marker it never takes focus, lets every click through and sends nothing to the game.

import (
	"log"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/freakyfish42/champ-select-companion/core"
	"golang.org/x/sys/windows"
)

var (
	pBeginPaint   = user32.NewProc("BeginPaint")
	pEndPaint     = user32.NewProc("EndPaint")
	pDrawTextW    = user32.NewProc("DrawTextW")
	pCreateFontW  = gdi32.NewProc("CreateFontW")
	pSelectObject = gdi32.NewProc("SelectObject")
	pSetTextColor = gdi32.NewProc("SetTextColor")
	pSetBkMode    = gdi32.NewProc("SetBkMode")
)

const (
	wmPaint        = 0x000F
	dtCenter       = 0x01
	dtVCenter      = 0x04
	dtSingleLine   = 0x20
	bkTransparent  = 1
	cdBackground   = 0x00140D08 // dark blue-black (0x00BBGGRR)
	cdText         = 0x0078C8F0 // warm gold
	cdTextSoon     = 0x00808CFF // soft red for the last 10 seconds
	cdTextUp       = 0x0090E6A0 // green once it's up
	objectiveTopAt = 0.035      // the box's top, down from the game's top, in game heights
)

type objectiveBox struct {
	mu     sync.Mutex
	win    uintptr
	bg     uintptr
	font   uintptr
	fontH  int32
	text   string
	colour uint32
}

var objective = &objectiveBox{}

// objectivePaint draws the box (called from markerProc for this window's WM_PAINT, on the UI thread).
func objectivePaint(h uintptr) {
	var ps [72]byte // PAINTSTRUCT; the device context is its first field
	hdc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
	objective.mu.Lock()
	text, colour, font, bg := objective.text, objective.colour, objective.font, objective.bg
	objective.mu.Unlock()
	var r rect
	pGetClientRectW.Call(h, uintptr(unsafe.Pointer(&r)))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), bg)
	if text == "" || font == 0 {
		return
	}
	old, _, _ := pSelectObject.Call(hdc, font)
	pSetBkMode.Call(hdc, bkTransparent)
	pSetTextColor.Call(hdc, uintptr(colour))
	t, _ := windows.UTF16FromString(text)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&t[0])), uintptr(len(t)-1), uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine)
	pSelectObject.Call(hdc, old)
}

// makeObjective creates the window, hidden, with the marker's window class. Call it on the UI thread after
// makeMarker.
func makeObjective(inst uintptr, cls *uint16) {
	h, _, err := pCreateWindowExW.Call(markerEx(false), uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)),
		wsPopup, 0, 0, 200, 40, 0, 0, inst, 0)
	if h == 0 {
		log.Printf("objective countdown: create window: %v", err)
		return
	}
	bg, _, _ := pCreateSolidBrush.Call(cdBackground)
	objective.mu.Lock()
	objective.win, objective.bg = h, bg
	objective.mu.Unlock()
}

// setFont makes the font for a box height (once per size).
func (o *objectiveBox) setFont(px int32) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.font != 0 && o.fontH == px {
		return
	}
	face, _ := windows.UTF16PtrFromString("Segoe UI Semibold")
	f, _, _ := pCreateFontW.Call(uintptr(-px), 0, 0, 0, 600, 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0, uintptr(unsafe.Pointer(face)))
	if f == 0 {
		return
	}
	if o.font != 0 {
		pDeleteObject.Call(o.font)
	}
	o.font, o.fontH = f, px
}

// objectiveRect: centred at the top of the game, sized by its height like League's own HUD.
func objectiveRect(game rect) rect {
	h := float64(game.Bottom - game.Top)
	bw, bh := math.Round(h*0.30), math.Round(h*0.055)
	mid := float64(game.Left+game.Right) / 2
	l := int32(math.Round(mid - bw/2))
	t := game.Top + int32(math.Round(h*objectiveTopAt))
	return rect{Left: l, Top: t, Right: l + int32(bw), Bottom: t + int32(bh)}
}

// objectiveLoop reads the game clock about once a second while a game with the card runs, and shows the
// countdown when an objective is within a minute of its first spawn.
func (a *App) objectiveLoop() {
	var clock float64     // game time at the last read
	var clockAt time.Time // when it was read
	var last string
	shaped := rect{}
	for {
		objective.mu.Lock()
		win := objective.win
		objective.mu.Unlock()
		if win == 0 {
			time.Sleep(time.Second)
			continue
		}
		a.mu.Lock()
		on := a.inGame && a.cardReady() && !a.cardHidden && !a.cfg.ObjectivesOff && a.queue != "Swiftplay"
		live := a.live
		panel := a.hwnd
		a.mu.Unlock()
		if on && live != nil && time.Since(clockAt) > time.Second {
			if t, err := live.GameTime(); err == nil {
				clock, clockAt = t, time.Now()
			}
		}
		g, fg := gameWindow(), foreground()
		gr, ok := clientRect(g)
		up := on && !clockAt.IsZero() && time.Since(clockAt) < 5*time.Second && g != 0 && ok && !isIconic(g) && isVisible(g) && (fg == g || fg == panel)
		text := ""
		var o core.Objective
		var left float64
		if up {
			now := clock + time.Since(clockAt).Seconds()
			var show bool
			if o, left, show = core.Countdown(now); show {
				text = core.CountdownText(o, left)
			}
		}
		if text == "" {
			if isVisible(win) {
				hideWindow(win)
			}
			if !on {
				clockAt = time.Time{} // a new game starts from a fresh clock
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		r := objectiveRect(gr)
		if r != shaped {
			objective.setFont(int32(float64(r.Bottom-r.Top) * 0.62))
			round := uintptr(max32(8, (r.Bottom-r.Top)/3))
			rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(r.Right-r.Left+1), uintptr(r.Bottom-r.Top+1), round, round)
			pSetWindowRgn.Call(win, rgn, 1)
			shaped = r
		}
		colour := uint32(cdText)
		alpha := 215.0
		switch {
		case left <= 0:
			colour = cdTextUp
		case left <= 10: // the last ten seconds pulse
			colour = cdTextSoon
			alpha = 190 + 65*(0.5+0.5*math.Sin(2*math.Pi*time.Since(clockAt).Seconds()/0.8))
		}
		objective.mu.Lock()
		objective.text, objective.colour = text, colour
		objective.mu.Unlock()
		if text != last {
			pInvalidateRect.Call(win, 0, 1)
			last = text
		}
		pSetLayeredAttrs.Call(win, 0, uintptr(alpha), lwaAlpha)
		if cur, ok := windowRect(win); !ok || cur != r || !isVisible(win) {
			placeTopmost(win, r, true)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (a *App) setObjectivesOff(off bool) {
	a.mu.Lock()
	a.cfg.ObjectivesOff = off
	a.mu.Unlock()
	a.saveConfig()
}
