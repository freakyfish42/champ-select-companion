package main

// Thin Win32 helpers: finding the League client, placing the panel next to it, message boxes,
// the "start with Windows" setting and the single-instance lock.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/freakyfish42/champ-select-companion/core"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	dwmapi                 = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	pDwmBlurBehind         = dwmapi.NewProc("DwmEnableBlurBehindWindow")
	pCreateRectRgn         = gdi32.NewProc("CreateRectRgn")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pGetStockObject        = gdi32.NewProc("GetStockObject")
	pSetClassLongPtrW      = user32.NewProc("SetClassLongPtrW")
	pFindWindowW           = user32.NewProc("FindWindowW")
	pGetWindowRect         = user32.NewProc("GetWindowRect")
	pIsIconic              = user32.NewProc("IsIconic")
	pIsWindowVisible       = user32.NewProc("IsWindowVisible")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pShowWindow            = user32.NewProc("ShowWindow")
	pGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	pSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	pReleaseCapture        = user32.NewProc("ReleaseCapture")
	pPostMessageW          = user32.NewProc("PostMessageW")
	pMonitorFromWindow     = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	pMessageBoxW           = user32.NewProc("MessageBoxW")
	pSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	pSetLayeredAttrs       = user32.NewProc("SetLayeredWindowAttributes")
	pGetAsyncKeyState      = user32.NewProc("GetAsyncKeyState")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
)

type rect struct{ Left, Top, Right, Bottom int32 }

const (
	swHide           = 0
	swShowNoActivate = 4
	swpNoSize        = 0x0001
	swpNoMove        = 0x0002
	swpNoActivate    = 0x0010
	swpFrameChanged  = 0x0020
	swpShowWindow    = 0x0040
	hwndTopmost      = ^uintptr(0)  // -1
	gwlStyle         = ^uintptr(15) // -16
	gwlExStyle       = ^uintptr(19) // -20
	wsPopup          = 0x80000000
	wsClipChildren   = 0x02000000
	wsExToolWindow   = 0x00000080
	wmNCLButtonDown  = 0x00A1
	htCaption        = 2
	monitorNearest   = 2
	mbIconInfo       = 0x40
	mbIconWarning    = 0x30
	mbTopmost        = 0x40000
	wsExLayered      = 0x00080000
	wsExTransparent  = 0x00000020 // clicks go through to the game
	wsExNoActivate   = 0x08000000
	wsExTopmost      = 0x00000008
	lwaAlpha         = 2
)

func setDPIAware() {
	if pSetProcessDpiAwareCtx.Find() == nil {
		pSetProcessDpiAwareCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
}

func messageBox(text string, icon uint32) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Champ Select Companion")
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(icon|mbTopmost))
}

// askYesNo shows a Yes/No message and reports whether Yes was picked.
func askYesNo(text string, icon uint32) bool {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Champ Select Companion")
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(icon|mbTopmost|0x4)) // MB_YESNO
	return r == 6                                                                                                        // IDYES
}

func openURL(u string) {
	_ = windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(u), nil, nil, 1)
}

// findLeague reads the lockfile next to the running LeagueClientUx.exe.
// leagueDirs: where the League client runs from, then the usual install folder.
func leagueDirs() []string {
	dirs := []string{}
	if snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		var e windows.ProcessEntry32
		e.Size = uint32(unsafe.Sizeof(e))
		for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
			if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "LeagueClientUx.exe") {
				continue
			}
			if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID); err == nil {
				buf := make([]uint16, windows.MAX_LONG_PATH)
				n := uint32(len(buf))
				if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
					dirs = append(dirs, filepath.Dir(windows.UTF16ToString(buf[:n])))
				}
				windows.CloseHandle(h)
			}
			break
		}
		windows.CloseHandle(snap)
	}
	return append(dirs, `C:\Riot Games\League of Legends`)
}

// gameWindowMode: League's Fullscreen/Windowed/Borderless setting, from Config\game.cfg (read only).
func gameWindowMode() (int, bool) {
	for _, d := range leagueDirs() {
		if b, err := os.ReadFile(filepath.Join(d, "Config", "game.cfg")); err == nil {
			return core.GameWindowMode(string(b))
		}
	}
	return 0, false
}

func findLeague() (core.Creds, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return core.Creds{}, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "LeagueClientUx.exe") {
			continue
		}
		dir := ""
		if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID); err == nil {
			buf := make([]uint16, windows.MAX_LONG_PATH)
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
				dir = filepath.Dir(windows.UTF16ToString(buf[:n]))
			}
			windows.CloseHandle(h)
		}
		for _, d := range []string{dir, `C:\Riot Games\League of Legends`} {
			if d == "" {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(d, "lockfile")); err == nil {
				return core.ParseLockfile(string(b))
			}
		}
		return core.Creds{}, errors.New("found the League client but not its lockfile")
	}
	return core.Creds{}, errors.New("League isn't running")
}

// processRunning reports whether a program with this file name is running.
func processRunning(exe string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			return true
		}
	}
	return false
}

func clientWindow() uintptr {
	cls, _ := windows.UTF16PtrFromString("RCLIENT")
	ttl, _ := windows.UTF16PtrFromString("League of Legends")
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(ttl)))
	return h
}

// gameWindow is the running game (not the client), or 0.
func gameWindow() uintptr {
	cls, _ := windows.UTF16PtrFromString("RiotWindowClass")
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	return h
}

func windowRect(h uintptr) (rect, bool) {
	var r rect
	ok, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func isIconic(h uintptr) bool  { r, _, _ := pIsIconic.Call(h); return r != 0 }
func isVisible(h uintptr) bool { r, _, _ := pIsWindowVisible.Call(h); return r != 0 }
func foreground() uintptr      { r, _, _ := pGetForegroundWindow.Call(); return r }

func dpiScale(h uintptr) float64 {
	if pGetDpiForWindow.Find() != nil {
		return 1
	}
	d, _, _ := pGetDpiForWindow.Call(h)
	if d == 0 {
		return 1
	}
	return float64(d) / 96
}

func workArea(h uintptr) rect {
	mon, _, _ := pMonitorFromWindow.Call(h, monitorNearest)
	var mi struct {
		Size    uint32
		Monitor rect
		Work    rect
		Flags   uint32
	}
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi.Work
}

// panelRect: beside the client on the right, else on the left, else over the client's right edge.
func panelRect(client rect, work rect, scale float64) rect {
	w := int32(300 * scale)
	r := rect{Top: client.Top, Bottom: client.Bottom}
	switch {
	case client.Right+w <= work.Right:
		r.Left = client.Right
	case client.Left-w >= work.Left:
		r.Left = client.Left - w
	default:
		r.Left = client.Right - w
	}
	r.Right = r.Left + w
	return r
}

// makePanel turns the webview's normal window into a frameless tool window (no taskbar button), hidden.
func makePanel(h uintptr) {
	pShowWindow.Call(h, swHide)
	pSetWindowLongPtrW.Call(h, gwlStyle, wsPopup|wsClipChildren)
	pSetWindowLongPtrW.Call(h, gwlExStyle, wsExToolWindow)
	pSetWindowPos.Call(h, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged)
	// the window's own background is black: with see-through on (below), black counts as transparent, so only
	// what the page draws shows. The panel's page is opaque, so this changes nothing there.
	if pGetStockObject.Find() == nil && pSetClassLongPtrW.Find() == nil {
		black, _, _ := pGetStockObject.Call(4)        // BLACK_BRUSH
		pSetClassLongPtrW.Call(h, ^uintptr(9), black) // GCLP_HBRBACKGROUND (-10)
	}
	if pDwmSetWindowAttribute.Find() == nil {
		round := uint32(2) // DWMWCP_ROUND (Windows 11)
		pDwmSetWindowAttribute.Call(h, 33, uintptr(unsafe.Pointer(&round)), 4)
	}
}

func placeTopmost(h uintptr, r rect, move bool) {
	flags := uintptr(swpNoActivate | swpShowWindow)
	if !move {
		flags |= swpNoMove | swpNoSize
	}
	pSetWindowPos.Call(h, hwndTopmost, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), flags)
}

func hideWindow(h uintptr) { pShowWindow.Call(h, swHide) }

// setCardStyle switches the window between the champ select panel and the in-game card. The card never
// takes focus, is slightly see-through and, unless it's being moved, lets every click through to the game.
//
// seeThrough lets the game show through the card: the page's background is drawn half-transparent (the web
// view's own background is transparent, see uiLoop) and Windows blends the window per pixel (blur-behind with
// an empty region: no blur, just alpha). Off = solid, as in v1.0.1/v1.0.2.
func setCardStyle(h uintptr, card, clickThrough, seeThrough bool) {
	ex := uintptr(wsExToolWindow)
	if card {
		ex |= wsExLayered | wsExNoActivate
		if clickThrough {
			ex |= wsExTransparent
		}
	}
	pSetWindowLongPtrW.Call(h, gwlExStyle, ex)
	if card {
		pSetLayeredAttrs.Call(h, 0, 255, lwaAlpha)
	}
	perPixelAlpha(h, card && seeThrough)
	pSetWindowPos.Call(h, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged)
}

func perPixelAlpha(h uintptr, on bool) {
	if pDwmBlurBehind.Find() != nil || pCreateRectRgn.Find() != nil {
		return
	}
	var bb struct {
		Flags      uint32
		Enable     int32
		Region     uintptr
		Transition int32
	}
	bb.Flags = 0x1 // DWM_BB_ENABLE
	if on {
		bb.Enable = 1
		rgn, _, _ := pCreateRectRgn.Call(0, 0, ^uintptr(0), ^uintptr(0)) // empty: blend, don't blur
		defer pDeleteObject.Call(rgn)
		bb.Region, bb.Flags = rgn, 0x1|0x2 // | DWM_BB_BLURREGION
	}
	pDwmBlurBehind.Call(h, uintptr(unsafe.Pointer(&bb)))
}

// cardRect: see core.CardRect (tested there).
func cardRect(game rect, scale float64, dx, dy, cssH int32, size int) rect {
	r := core.CardRect(core.Rect(game), scale, dx, dy, cssH, size)
	return rect(r)
}

// The hotkey that hides and shows the card. Only watched while a game with a card is running.
const cardHotkey = "Ctrl+Shift+B"

const (
	vkShift   = 0x10
	vkControl = 0x11
)

// cursorPos: where the mouse pointer is (reading only; the app never moves it).
func cursorPos() (int32, int32, bool) {
	var p struct{ X, Y int32 }
	r, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y, r != 0
}

// cardKeysDown: are Ctrl, Shift and B held right now? (Key state only: no keyboard hook.)
func cardKeysDown() bool {
	down := func(vk uintptr) bool { r, _, _ := pGetAsyncKeyState.Call(vk); return r&0x8000 != 0 }
	return down(vkControl) && down(vkShift) && down('B')
}

// startDrag lets the player move the frameless panel by its header.
func startDrag(h uintptr) {
	pReleaseCapture.Call()
	pPostMessageW.Call(h, wmNCLButtonDown, htCaption, 0) // posted, not sent: we're inside a WebView2 callback
}

// ---- start with Windows ----

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "champ-select-companion"

// removeOldAutostart deletes the early test build's start-with-Windows entry.
func removeOldAutostart() {
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue("lomsland-companion")
		k.Close()
	}
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runName)
	return err == nil
}

func setAutostart(on bool, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue(runName, `"`+exe+`" --autostart`)
	}
	if err := k.DeleteValue(runName); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return err
	}
	return nil
}

// ---- one copy at a time ----

func singleInstance() (windows.Handle, bool) {
	name, _ := windows.UTF16PtrFromString(`Local\champ-select-companion`)
	h, err := windows.CreateMutex(nil, true, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return 0, false
	}
	return h, err == nil
}
