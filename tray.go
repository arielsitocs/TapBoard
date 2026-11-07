//go:build windows

package main

// Ícono en la bandeja (junto al reloj de Windows). Sirve sobre todo cuando el
// panel está escondido porque su monitor no está: desde aquí se puede mostrar
// el panel en otra pantalla, abrir la configuración o salir.

import (
	"unsafe"
)

var (
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")

	trayOn         bool
	trayRestartMsg uintptr // "TaskbarCreated": el Explorador se reinició
	panelForced    bool    // el usuario pidió ver el panel aunque falte su monitor
)

const (
	WM_APP_TRAY = WM_APP + 20

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2
	nifMsg    = 0x1
	nifIcon   = 0x2
	nifTip    = 0x4

	trayShow     = 1
	trayHide     = 2
	traySettings = 3
	trayExit     = 4
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

func trayData() notifyIconData {
	var d notifyIconData
	d.CbSize = uint32(unsafe.Sizeof(d))
	d.HWnd = hwnd
	d.UID = 1
	d.UFlags = nifMsg | nifIcon | nifTip
	d.UCallbackMessage = WM_APP_TRAY
	hInst, _, _ := pGetModuleHandleW.Call(0)
	d.HIcon = appIcon16(hInst, 16)
	tip := "TapBoard " + appVersion
	if panelHidden {
		tip += " · panel escondido (no está su monitor)"
	}
	for i, r := range []rune(tip) {
		if i >= len(d.SzTip)-1 {
			break
		}
		d.SzTip[i] = uint16(r)
	}
	return d
}

func addTrayIcon() {
	if trayRestartMsg == 0 {
		trayRestartMsg, _, _ = pRegisterWindowMessageW.Call(wstrPtr("TaskbarCreated"))
	}
	d := trayData()
	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
	trayOn = r != 0
}

// updateTrayTip refresca el texto al pasar el mouse (cambia si el panel se esconde).
func updateTrayTip() {
	if !trayOn {
		return
	}
	d := trayData()
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

func removeTrayIcon() {
	if !trayOn {
		return
	}
	d := trayData()
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	trayOn = false
}

// onTrayMessage atiende los clics sobre el ícono.
func onTrayMessage(lparam uintptr) {
	switch lparam & 0xFFFF {
	case 0x0202, 0x0205: // WM_LBUTTONUP, WM_RBUTTONUP
		showTrayMenu()
	}
}

func showTrayMenu() {
	ownerItems = nil
	m, _, _ := pCreatePopupMenu.Call()
	styleMenu(m)
	menuAppend(m, MF_STRING|0x0003, 0, "TapBoard v"+appVersion)
	menuAppend(m, MF_SEPARATOR, 0, "")
	switch {
	case panelHidden:
		menuAppend(m, MF_STRING, trayShow, "Mostrar el panel en esta pantalla")
	case panelForced:
		menuAppend(m, MF_STRING, trayHide, "Esconder el panel")
	}
	menuAppend(m, MF_STRING, traySettings, "Configuración…")
	menuAppend(m, MF_SEPARATOR, 0, "")
	menuAppend(m, MF_STRING, trayExit, "Salir")

	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)                                                        // para que el menú se cierre al tocar afuera
	cmd, _, _ := pTrackPopupMenu.Call(m, TPM_RETURNCMD|TPM_NONOTIFY|TPM_RIGHTALIGN|0x0020, // TPM_BOTTOMALIGN
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, WM_NULL, 0, 0)
	pDestroyMenu.Call(m)

	switch cmd {
	case trayShow:
		panelForced = true
		placeWindow()
	case trayHide:
		panelForced = false
		placeWindow()
	case traySettings:
		openSettingsWindow()
	case trayExit:
		pPostQuitMessage.Call(0)
	}
}
