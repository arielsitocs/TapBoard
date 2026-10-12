//go:build windows

package main

import (
	"math"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	powrprof = syscall.NewLazyDLL("powrprof.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pBeginPaint                    = user32.NewProc("BeginPaint")
	pEndPaint                      = user32.NewProc("EndPaint")
	pInvalidateRect                = user32.NewProc("InvalidateRect")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pSetTimer                      = user32.NewProc("SetTimer")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	pMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	pScreenToClient                = user32.NewProc("ScreenToClient")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pSetCursorPos                  = user32.NewProc("SetCursorPos")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pSetCapture                    = user32.NewProc("SetCapture")
	pReleaseCapture                = user32.NewProc("ReleaseCapture")
	pGetMessageExtraInfo           = user32.NewProc("GetMessageExtraInfo")
	pKeybdEvent                    = user32.NewProc("keybd_event")
	pCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                   = user32.NewProc("AppendMenuW")
	pTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                   = user32.NewProc("DestroyMenu")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pSendMessageW                  = user32.NewProc("SendMessageW")
	pIsWindow                      = user32.NewProc("IsWindow")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pIsZoomed                      = user32.NewProc("IsZoomed")
	pIsIconic                      = user32.NewProc("IsIconic")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")

	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW        = kernel32.NewProc("CreateMutexW")
	pGetCurrentProcess   = kernel32.NewProc("GetCurrentProcess")
	pGetCurrentProcessId = kernel32.NewProc("GetCurrentProcessId")
	pCloseHandle         = kernel32.NewProc("CloseHandle")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	pPropVariantClear = ole32.NewProc("PropVariantClear")

	pShellExecuteW        = shell32.NewProc("ShellExecuteW")
	pSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")

	pOpenProcessToken      = advapi32.NewProc("OpenProcessToken")
	pLookupPrivilegeValueW = advapi32.NewProc("LookupPrivilegeValueW")
	pAdjustTokenPrivileges = advapi32.NewProc("AdjustTokenPrivileges")
	pRegOpenKeyExW         = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW        = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW       = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW      = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey           = advapi32.NewProc("RegCloseKey")

	pSetSuspendState = powrprof.NewProc("SetSuspendState")
)

const (
	WS_POPUP   = 0x80000000
	WS_VISIBLE = 0x10000000

	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000

	SW_SHOWNOACTIVATE = 4
	SW_SHOWNORMAL     = 1

	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_PAINT          = 0x000F
	WM_ERASEBKGND     = 0x0014
	WM_MOUSEACTIVATE  = 0x0021
	WM_DISPLAYCHANGE  = 0x007E
	WM_TIMER          = 0x0113
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONUP      = 0x0202
	WM_RBUTTONUP      = 0x0205
	WM_CAPTURECHANGED = 0x0215
	WM_DPICHANGED     = 0x02E0
	WM_NULL           = 0x0000
	WM_SYSCOMMAND     = 0x0112
	WM_APP            = 0x8000

	WM_POINTERUPDATE         = 0x0245
	WM_POINTERDOWN           = 0x0246
	WM_POINTERUP             = 0x0247
	WM_POINTERENTER          = 0x0249
	WM_POINTERLEAVE          = 0x024A
	WM_POINTERACTIVATE       = 0x024B
	WM_POINTERCAPTURECHANGED = 0x024C

	WM_TABLET_QUERYSYSTEMGESTURESTATUS = 0x02CC

	MA_NOACTIVATE = 3
	PA_NOACTIVATE = 3

	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2

	SWP_NOACTIVATE = 0x0010
	SWP_SHOWWINDOW = 0x0040

	SRCCOPY    = 0x00CC0020
	CAPTUREBLT = 0x40000000

	IDC_ARROW = 32512

	MF_STRING    = 0x0000
	MF_SEPARATOR = 0x0800
	MF_CHECKED   = 0x0008

	TPM_RETURNCMD  = 0x0100
	TPM_NONOTIFY   = 0x0080
	TPM_RIGHTALIGN = 0x0008

	MONITOR_DEFAULTTOPRIMARY = 1

	VK_MEDIA_PLAY_PAUSE   = 0xB3
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002

	SC_MONITORPOWER = 0xF170
)

type POINT struct{ X, Y int32 }
type RECT struct{ L, T, R, B int32 }

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func f32(f float32) uintptr { return uintptr(math.Float32bits(f)) }

func wstr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func wstrPtr(s string) uintptr { return uintptr(unsafe.Pointer(wstr(s))) }

// utf16PtrToString lee una cadena UTF-16 terminada en cero.
func utf16PtrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var buf []uint16
	for i := uintptr(0); ; i += 2 {
		c := *(*uint16)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		buf = append(buf, c)
		if len(buf) > 4096 {
			break
		}
	}
	return syscall.UTF16ToString(buf)
}

func loword(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func xParam(l uintptr) int32  { return int32(int16(l & 0xFFFF)) }
func yParam(l uintptr) int32  { return int32(int16((l >> 16) & 0xFFFF)) }

func invalidate(hwnd uintptr) { pInvalidateRect.Call(hwnd, 0, 0) }

func getCursorPos() POINT {
	var p POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func foregroundWindow() uintptr {
	r, _, _ := pGetForegroundWindow.Call()
	return r
}

func messageBox(text, title string) {
	pMessageBoxW.Call(0, wstrPtr(text), wstrPtr(title), 0x40)
}

func parseGUID(s string) GUID {
	// formato: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	hex := func(c byte) byte {
		switch {
		case c >= '0' && c <= '9':
			return c - '0'
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10
		}
		return 0
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '-' || s[i] == '{' || s[i] == '}' {
			continue
		}
		b = append(b, s[i])
	}
	var raw [16]byte
	for i := 0; i < 16 && 2*i+1 < len(b); i++ {
		raw[i] = hex(b[2*i])<<4 | hex(b[2*i+1])
	}
	var g GUID
	g.Data1 = uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	g.Data2 = uint16(raw[4])<<8 | uint16(raw[5])
	g.Data3 = uint16(raw[6])<<8 | uint16(raw[7])
	copy(g.Data4[:], raw[8:16])
	return g
}

// comCall llama al método número idx de la vtable de un objeto COM.
func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, obj)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

func knownFolder(id string) string {
	g := parseGUID(id)
	var p uintptr
	r, _, _ := pSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&g)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == 0 {
		return ""
	}
	s := utf16PtrToString(p)
	pCoTaskMemFree.Call(p)
	return s
}

const (
	FOLDERID_Pictures  = "33E28130-4E1E-4676-835A-98395C3BC3BB"
	FOLDERID_Downloads = "374DE290-123F-4565-9164-39C4925E467B"
)

func shellOpen(path string) {
	pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(path), 0, 0, SW_SHOWNORMAL)
}

func enableShutdownPrivilege() {
	proc, _, _ := pGetCurrentProcess.Call()
	var tok uintptr
	r, _, _ := pOpenProcessToken.Call(proc, 0x20|0x8, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return
	}
	defer pCloseHandle.Call(tok)
	type luidAttr struct {
		Low   uint32
		High  int32
		Attrs uint32
	}
	type tokenPrivileges struct {
		Count uint32
		Priv  luidAttr
	}
	var tp tokenPrivileges
	tp.Count = 1
	tp.Priv.Attrs = 2 // SE_PRIVILEGE_ENABLED
	pLookupPrivilegeValueW.Call(0, wstrPtr("SeShutdownPrivilege"), uintptr(unsafe.Pointer(&tp.Priv)))
	pAdjustTokenPrivileges.Call(tok, 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
}

func sendMediaPlayPause() {
	pKeybdEvent.Call(VK_MEDIA_PLAY_PAUSE, 0, KEYEVENTF_EXTENDEDKEY, 0)
	pKeybdEvent.Call(VK_MEDIA_PLAY_PAUSE, 0, KEYEVENTF_EXTENDEDKEY|KEYEVENTF_KEYUP, 0)
}

// ---- Registro: iniciar con Windows ----

const (
	HKEY_CURRENT_USER = 0x80000001
	KEY_READ          = 0x20019
	KEY_WRITE         = 0x20006
	REG_SZ            = 1
	runKey            = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue          = "TapBoard"
)

// migrateAutostart: si la versión anterior (Panel Táctil) iniciaba con Windows,
// se pasa a TapBoard con la ruta del .exe actual.
func migrateAutostart(exe string) {
	var hk uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, wstrPtr(runKey), 0, KEY_READ|KEY_WRITE, uintptr(unsafe.Pointer(&hk)))
	if r != 0 {
		return
	}
	r, _, _ = pRegQueryValueExW.Call(hk, wstrPtr("PanelTactil"), 0, 0, 0, 0)
	if r == 0 {
		pRegDeleteValueW.Call(hk, wstrPtr("PanelTactil"))
		pRegCloseKey.Call(hk)
		setAutostart(true, exe)
		return
	}
	pRegCloseKey.Call(hk)
}

func autostartEnabled() bool {
	var hk uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, wstrPtr(runKey), 0, KEY_READ, uintptr(unsafe.Pointer(&hk)))
	if r != 0 {
		return false
	}
	defer pRegCloseKey.Call(hk)
	r, _, _ = pRegQueryValueExW.Call(hk, wstrPtr(runValue), 0, 0, 0, 0)
	return r == 0
}

func setAutostart(on bool, exe string) {
	var hk uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, wstrPtr(runKey), 0, KEY_WRITE, uintptr(unsafe.Pointer(&hk)))
	if r != 0 {
		return
	}
	defer pRegCloseKey.Call(hk)
	if on {
		val, _ := syscall.UTF16FromString(`"` + exe + `"`)
		pRegSetValueExW.Call(hk, wstrPtr(runValue), 0, REG_SZ,
			uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
	} else {
		pRegDeleteValueW.Call(hk, wstrPtr(runValue))
	}
}
