//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// --- Win32 DLL procs ------------------------------------------------

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procUnregisterClassW    = modUser32.NewProc("UnregisterClassW")
	procCreateWindowExW     = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW      = modUser32.NewProc("DefWindowProcW")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = modUser32.NewProc("DispatchMessageW")
	procPostMessageW        = modUser32.NewProc("PostMessageW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procLoadIconW           = modUser32.NewProc("LoadIconW")
	procCreatePopupMenu     = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW         = modUser32.NewProc("AppendMenuW")
	procDestroyMenu         = modUser32.NewProc("DestroyMenu")
	procTrackPopupMenu      = modUser32.NewProc("TrackPopupMenu")
	procGetCursorPos        = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")

	procShellNotifyIconW = modShell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

// --- Win32 constants ------------------------------------------------

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmCommand      = 0x0111
	wmLButtonDown  = 0x0201
	wmLButtonDblClk = 0x0203
	wmRButtonUp    = 0x0205
	wmUser         = 0x0400

	wmTrayIcon = wmUser + 1 // callback message set in NOTIFYICONDATA
	wmAppStop  = wmUser + 2 // posted by Stop() from another goroutine

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	idiApplication = 32512 // stock IDI_APPLICATION — built-in app icon

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfChecked   = 0x0008

	tpmRightButton = 0x0002
	tpmLeftAlign   = 0x0000

	// Menu command IDs. Kept small because they travel in the low 16
	// bits of WM_COMMAND's WPARAM.
	cmdOpen            = 1
	cmdToggleAutostart = 2
	cmdQuit            = 99
)

// --- Win32 struct layouts ------------------------------------------
//
// Field order and padding match the C layouts. Do not re-order.

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type point struct {
	X, Y int32
}

type msg struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// --- state ---------------------------------------------------------

// active is the single live tray for the process. The Win32 WNDPROC
// callback is created once and dispatches through this pointer. A nil
// pointer at callback time means a message was delivered after Stop
// detached the tray; such messages are handled by DefWindowProcW.
var active atomic.Pointer[winTray]

// wndProcCallback is the one-time syscall.NewCallback conversion of
// our Go WNDPROC. Creating callbacks is not free (each one costs a
// locked system thread slot), and we only ever have one tray per
// process, so we allocate one.
var wndProcCallback = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(wndProc)
})

type winTray struct {
	cfg Config

	hInstance windows.Handle
	hWnd      windows.Handle
	hIcon     windows.Handle
	classAtom uintptr

	classNamePtr *uint16 // retained for UnregisterClass
	windowName   *uint16 // retained for CreateWindowExW

	stopping atomic.Bool
}

func stop() {
	t := active.Load()
	if t == nil {
		return
	}
	t.stopping.Store(true)
	// PostMessage is thread-safe; sending WM_APP_STOP makes the
	// message-loop thread call DestroyWindow, which in turn posts
	// WM_DESTROY and WM_QUIT. Safe to call even if hWnd is already
	// destroyed — PostMessage to a dead HWND just returns false.
	_, _, _ = procPostMessageW.Call(uintptr(t.hWnd), uintptr(wmAppStop), 0, 0)
}

func run(cfg Config) error {
	// Win32 GUI requires window owner thread == message pump thread.
	// We lock the invoking goroutine here; we never unlock so the
	// runtime doesn't reuse this thread after Run returns.
	runtime.LockOSThread()

	t := &winTray{cfg: cfg}

	hInst, _, err := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		return fmt.Errorf("tray: GetModuleHandle: %w", err)
	}
	t.hInstance = windows.Handle(hInst)

	// Stock app icon. Replacing with a custom .ico is Session 6
	// polish; IDI_APPLICATION always loads and covers our needs.
	hIcon, _, err := procLoadIconW.Call(0, uintptr(idiApplication))
	if hIcon == 0 {
		return fmt.Errorf("tray: LoadIcon(IDI_APPLICATION): %w", err)
	}
	t.hIcon = windows.Handle(hIcon)

	// Register a unique class name so repeated Run calls (e.g., under
	// tests) don't trip "class already exists". The name is anchored
	// on the process PID — one tray per process, per spec.
	className := fmt.Sprintf("ShelfTrayWindow_%d", windows.GetCurrentProcessId())
	classNamePtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return fmt.Errorf("tray: class name: %w", err)
	}
	t.classNamePtr = classNamePtr

	wc := wndClassExW{
		LpfnWndProc:   wndProcCallback(),
		HInstance:     t.hInstance,
		HIcon:         t.hIcon,
		LpszClassName: t.classNamePtr,
		HIconSm:       t.hIcon,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))

	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))) // #nosec G103 -- syscall pointer to stack struct live through Call.
	if atom == 0 {
		return fmt.Errorf("tray: RegisterClassEx: %w", err)
	}
	t.classAtom = atom

	// Message-only windows never show on screen. Passing HWND_MESSAGE
	// (-3) as the parent is the documented pattern for invisible
	// utility windows that still receive messages.
	const hwndMessage = ^uintptr(2) // HWND_MESSAGE == -3

	windowName, err := syscall.UTF16PtrFromString("Shelf")
	if err != nil {
		return fmt.Errorf("tray: window name: %w", err)
	}
	t.windowName = windowName

	hWnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(t.classNamePtr)), // #nosec G103 -- syscall pointer; retained on *winTray for the call.
		uintptr(unsafe.Pointer(t.windowName)),   // #nosec G103 -- syscall pointer; retained on *winTray for the call.
		0,
		0, 0, 0, 0,
		hwndMessage,
		0,
		uintptr(t.hInstance),
		0,
	)
	if hWnd == 0 {
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(t.classNamePtr)), uintptr(t.hInstance)) // #nosec G103 -- syscall pointer; retained on *winTray.
		return fmt.Errorf("tray: CreateWindowEx: %w", err)
	}
	t.hWnd = windows.Handle(hWnd)

	active.Store(t)
	defer active.Store(nil)

	if err := t.addIcon(); err != nil {
		_, _, _ = procDestroyWindow.Call(uintptr(t.hWnd))
		return err
	}

	// Message loop. GetMessageW returns:
	//   > 0  normal message
	//   = 0  WM_QUIT (posted by PostQuitMessage in WM_DESTROY path)
	//   < 0  error
	var m msg
	for {
		ret, _, getErr := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)), // #nosec G103 -- syscall pointer; m is stack-local for Call duration.
			0, 0, 0,
		)
		// GetMessageW documents three ret values — 0, -1, positive —
		// so the uintptr-to-int32 cast is bounded by the API contract.
		rc := int32(ret) // #nosec G115 -- GetMessageW returns BOOL/-1; range-safe.
		if rc == -1 {
			return fmt.Errorf("tray: GetMessage: %w", getErr)
		}
		if rc == 0 {
			// WM_QUIT — clean shutdown.
			return nil
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m))) // #nosec G103 -- syscall pointer; stack-local.
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) // #nosec G103 -- syscall pointer; stack-local.
	}
}

func (t *winTray) addIcon() error {
	nid := t.notifyIconData(nifMessage | nifIcon | nifTip)
	return t.shellNotify(nimAdd, &nid)
}

func (t *winTray) removeIcon() {
	nid := t.notifyIconData(0)
	_ = t.shellNotify(nimDelete, &nid)
}

// notifyIconData composes the Shell_NotifyIcon parameter for this
// tray. Tooltip is truncated to 127 UTF-16 units (leaving room for
// the trailing NUL) — Windows silently fails to set longer tips.
func (t *winTray) notifyIconData(flags uint32) notifyIconDataW {
	nid := notifyIconDataW{
		HWnd:             t.hWnd,
		UID:              1,
		UFlags:           flags,
		UCallbackMessage: wmTrayIcon,
		HIcon:            t.hIcon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))

	tooltip := t.cfg.Tooltip
	if tooltip == "" {
		tooltip = "Shelf"
	}
	u16, err := syscall.UTF16FromString(tooltip)
	if err == nil {
		n := len(u16)
		if n > len(nid.SzTip)-1 {
			n = len(nid.SzTip) - 1
		}
		copy(nid.SzTip[:n], u16[:n])
		// Ensure NUL terminator; [128]uint16 default is 0 but be explicit.
		nid.SzTip[n] = 0
	}
	return nid
}

func (t *winTray) shellNotify(action uint32, nid *notifyIconDataW) error {
	ret, _, err := procShellNotifyIconW.Call(uintptr(action), uintptr(unsafe.Pointer(nid))) // #nosec G103 -- syscall pointer; nid is caller-owned and live through Call.
	if ret == 0 {
		return fmt.Errorf("tray: Shell_NotifyIcon(action=%d): %w", action, err)
	}
	return nil
}

// wndProc is the package-level WNDPROC. Called by Windows on the
// message-loop thread whenever a message targets our window. It
// cannot hold Go-specific state other than via the active pointer;
// Win32 passes no cookie.
func wndProc(hwnd windows.Handle, message uint32, wparam, lparam uintptr) uintptr {
	t := active.Load()
	if t == nil {
		ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
		return ret
	}

	switch message {
	case wmTrayIcon:
		// Low word of lparam is the mouse-event code.
		switch uint32(lparam & 0xFFFF) {
		case wmLButtonDown, wmLButtonDblClk:
			if t.cfg.OnOpen != nil {
				go t.cfg.OnOpen()
			}
		case wmRButtonUp:
			t.showMenu()
		}
		return 0

	case wmCommand:
		t.handleCommand(uint32(wparam & 0xFFFF))
		return 0

	case wmAppStop, wmClose:
		_, _, _ = procDestroyWindow.Call(uintptr(hwnd))
		return 0

	case wmDestroy:
		t.removeIcon()
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
	return ret
}

func (t *winTray) showMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu) //nolint:errcheck // destroy is best-effort on teardown.

	appendMenu(hMenu, mfString, cmdOpen, "Open Shelf")
	appendMenu(hMenu, mfSeparator, 0, "")

	autoFlag := uint32(mfString)
	if t.cfg.IsAutostartEnabled != nil && t.cfg.IsAutostartEnabled() {
		autoFlag |= mfChecked
	}
	appendMenu(hMenu, autoFlag, cmdToggleAutostart, "Start with Windows")

	appendMenu(hMenu, mfSeparator, 0, "")
	appendMenu(hMenu, mfString, cmdQuit, "Quit")

	// SetForegroundWindow is required so the popup dismisses when the
	// user clicks elsewhere. Without it, a tray-owned popup can get
	// stuck on screen.
	_, _, _ = procSetForegroundWindow.Call(uintptr(t.hWnd))

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))) // #nosec G103 -- syscall pointer; stack-local POINT receives coords.

	// POINT coords returned by GetCursorPos are screen pixels on a
	// monitor — always positive, always fit in 32 bits. Sign-extend is
	// not a concern here.
	_, _, _ = procTrackPopupMenu.Call(
		hMenu,
		tpmRightButton|tpmLeftAlign,
		uintptr(pt.X), // #nosec G115 -- screen-pixel coord, always >= 0.
		uintptr(pt.Y), // #nosec G115 -- screen-pixel coord, always >= 0.
		0,
		uintptr(t.hWnd),
		0,
	)
}

func (t *winTray) handleCommand(id uint32) {
	switch id {
	case cmdOpen:
		if t.cfg.OnOpen != nil {
			go t.cfg.OnOpen()
		}
	case cmdToggleAutostart:
		cur := false
		if t.cfg.IsAutostartEnabled != nil {
			cur = t.cfg.IsAutostartEnabled()
		}
		if t.cfg.OnToggleAutostart != nil {
			// Run on a fresh goroutine: a registry write can block on
			// antivirus and we must not stall the message loop.
			go func(target bool) {
				_ = t.cfg.OnToggleAutostart(target)
			}(!cur)
		}
	case cmdQuit:
		t.stopping.Store(true)
		if t.cfg.OnQuit != nil {
			go t.cfg.OnQuit()
		}
		_, _, _ = procDestroyWindow.Call(uintptr(t.hWnd))
	}
}

func appendMenu(hMenu uintptr, flags uint32, id uintptr, text string) {
	if text == "" {
		_, _, _ = procAppendMenuW.Call(hMenu, uintptr(flags), id, 0)
		return
	}
	ptr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _, _ = procAppendMenuW.Call(hMenu, uintptr(flags), id, uintptr(unsafe.Pointer(ptr))) // #nosec G103 -- syscall pointer; ptr lives through Call.
}

