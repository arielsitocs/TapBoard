//go:build windows

package main

// Atajos de teclado, escribir texto y grabar una combinación de teclas.

import (
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	pSendInput           = user32.NewProc("SendInput")
	pSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32.NewProc("CallNextHookEx")
)

type kbInput struct {
	Type  uint32
	_     uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	_     uint32
	Extra uintptr
	_     [8]byte
}

const (
	kfExtended = 0x1
	kfUp       = 0x2
	kfUnicode  = 0x4
)

var keyNames = map[string]uint16{
	"Ctrl": 0x11, "Shift": 0x10, "Alt": 0x12, "Win": 0x5B,
	"Esc": 0x1B, "Tab": 0x09, "Enter": 0x0D, "Espacio": 0x20, "Retroceso": 0x08,
	"Supr": 0x2E, "Insert": 0x2D, "Inicio": 0x24, "Fin": 0x23, "RePág": 0x21, "AvPág": 0x22,
	"←": 0x25, "↑": 0x26, "→": 0x27, "↓": 0x28, "ImprPant": 0x2C, "Pausa": 0x13,
	"Num*": 0x6A, "Num+": 0x6B, "Num-": 0x6D, "Num.": 0x6E, "Num/": 0x6F,
	";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF, "`": 0xC0,
	"[": 0xDB, "\\": 0xDC, "]": 0xDD, "'": 0xDE,
	"Vol+": 0xAF, "Vol-": 0xAE, "Mudo": 0xAD, "Play": 0xB3, "Sig.": 0xB0, "Ant.": 0xB1,
}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		keyNames[string(c)] = uint16(c)
	}
	for c := '0'; c <= '9'; c++ {
		keyNames[string(c)] = uint16(c)
		keyNames["Num"+string(c)] = uint16(0x60 + c - '0')
	}
	for i := 1; i <= 24; i++ {
		keyNames["F"+itoa(i)] = uint16(0x6F + i)
	}
}

func vkName(vk uint16) string {
	switch vk {
	case 0x5C:
		return "Win"
	case 0xA0, 0xA1:
		return "Shift"
	case 0xA2, 0xA3:
		return "Ctrl"
	case 0xA4, 0xA5:
		return "Alt"
	}
	for k, v := range keyNames {
		if v == vk && k != "\\" {
			return k
		}
	}
	if vk == 0xDC {
		return "\\"
	}
	return "Tecla" + itoa(int(vk))
}

func isExtended(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E, 0x5B, 0x5C, 0x6F, 0xAD, 0xAE, 0xAF, 0xB0, 0xB1, 0xB3:
		return true
	}
	return false
}

func sendInputs(in []kbInput) {
	if len(in) == 0 {
		return
	}
	pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// sendHotkey presiona una combinación como "Ctrl+Shift+M".
func sendHotkey(keys string) bool {
	var vks []uint16
	for _, p := range strings.Split(keys, "+") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		vk, ok := keyNames[p]
		if !ok {
			return false
		}
		vks = append(vks, vk)
	}
	if len(vks) == 0 {
		return false
	}
	var in []kbInput
	for _, vk := range vks {
		f := uint32(0)
		if isExtended(vk) {
			f |= kfExtended
		}
		in = append(in, kbInput{Type: 1, Vk: vk, Flags: f})
	}
	for i := len(vks) - 1; i >= 0; i-- {
		f := uint32(kfUp)
		if isExtended(vks[i]) {
			f |= kfExtended
		}
		in = append(in, kbInput{Type: 1, Vk: vks[i], Flags: f})
	}
	sendInputs(in)
	return true
}

// sendText escribe un texto en la ventana activa (cualquier carácter).
func sendText(t string) {
	var in []kbInput
	for _, r := range t {
		if r == '\n' {
			in = append(in, kbInput{Type: 1, Vk: 0x0D}, kbInput{Type: 1, Vk: 0x0D, Flags: kfUp})
			continue
		}
		if r == '\r' {
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			in = append(in, kbInput{Type: 1, Scan: u, Flags: kfUnicode}, kbInput{Type: 1, Scan: u, Flags: kfUnicode | kfUp})
		}
	}
	sendInputs(in)
}

// ---------------------------------------------------------------- grabar atajo

var (
	recHook  uintptr
	recMods  = map[string]bool{}
	recOnSet func(string)
	recBusy  bool
)

func modName(vk uint32) string {
	switch vk {
	case 0x10, 0xA0, 0xA1:
		return "Shift"
	case 0x11, 0xA2, 0xA3:
		return "Ctrl"
	case 0x12, 0xA4, 0xA5:
		return "Alt"
	case 0x5B, 0x5C:
		return "Win"
	}
	return ""
}

var recCB = syscall.NewCallback(func(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode < 0 || !recBusy {
		r, _, _ := pCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return r
	}
	vk := *(*uint32)(unsafe.Pointer(lParam))
	down := wParam == 0x100 || wParam == 0x104
	if m := modName(vk); m != "" {
		if down {
			recMods[m] = true
		} else {
			delete(recMods, m)
		}
		invalidate(hwnd)
		return 1 // no dejar pasar la tecla mientras se graba
	}
	if down {
		if vk == 0x1B && len(recMods) == 0 { // Esc solo = cancelar
			stopRecording()
			return 1
		}
		var parts []string
		for _, m := range []string{"Ctrl", "Shift", "Alt", "Win"} {
			if recMods[m] {
				parts = append(parts, m)
			}
		}
		parts = append(parts, vkName(uint16(vk)))
		combo := strings.Join(parts, "+")
		cb := recOnSet
		stopRecording()
		if cb != nil {
			cb(combo)
		}
	}
	return 1
})

func startRecording(onSet func(string)) {
	if recBusy {
		return
	}
	hInst, _, _ := pGetModuleHandleW.Call(0)
	h, _, _ := pSetWindowsHookExW.Call(13, recCB, hInst, 0) // WH_KEYBOARD_LL
	if h == 0 {
		toast("No se pudo grabar el teclado")
		return
	}
	recHook, recBusy, recOnSet = h, true, onSet
	recMods = map[string]bool{}
	invalidate(hwnd)
}

func stopRecording() {
	if recHook != 0 {
		pUnhookWindowsHookEx.Call(recHook)
	}
	recHook, recBusy, recOnSet = 0, false, nil
	recMods = map[string]bool{}
	invalidate(hwnd)
}

func recordingPreview() string {
	var parts []string
	for _, m := range []string{"Ctrl", "Shift", "Alt", "Win"} {
		if recMods[m] {
			parts = append(parts, m)
		}
	}
	if len(parts) == 0 {
		return "Presiona la combinación…"
	}
	return strings.Join(parts, "+") + "+…"
}
