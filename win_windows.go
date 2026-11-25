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

// askYesNoCancel returns 6 (Yes), 7 (No) or 2 (Cancel, or the box was closed).
func askYesNoCancel(text string, icon uint32) uintptr {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Champ Select Companion")
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(icon|mbTopmost|0x3)) // MB_YESNOCANCEL
	return r
}

func openURL(u string) {
	_ = windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(u), nil, nil, 1)
}

// findLeague reads the lockfile next to the running LeagueClientUx.exe.
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
