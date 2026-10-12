//go:build windows

package main

const appVersion = "2.1.0"

var (
	pGetClassNameW   = user32.NewProc("GetClassNameW")
	pGetWindowTextW  = user32.NewProc("GetWindowTextW")
	pKillTimer       = user32.NewProc("KillTimer")
	pLockWorkStation = user32.NewProc("LockWorkStation")
)

func sendMediaKey(vk uintptr) {
	pKeybdEvent.Call(vk, 0, KEYEVENTF_EXTENDEDKEY, 0)
	pKeybdEvent.Call(vk, 0, KEYEVENTF_EXTENDEDKEY|KEYEVENTF_KEYUP, 0)
}

var pLoadImageW = user32.NewProc("LoadImageW")

// appIcon16 carga el ícono del .exe (recurso 1) al tamaño pedido.
func appIcon16(hInst uintptr, size int) uintptr {
	h, _, _ := pLoadImageW.Call(hInst, 1, 1, uintptr(size), uintptr(size), 0) // IMAGE_ICON
	return h
}
