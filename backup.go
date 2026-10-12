//go:build windows

package main

// Respaldo: exportar e importar la configuración completa.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	comdlg32          = syscall.NewLazyDLL("comdlg32.dll")
	pGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")
	pGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")

	setMsg      string
	setMsgUntil time.Time
)

type openFileName struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	FnHook        uintptr
	TemplateName  *uint16
	PvReserved    uintptr
	DwReserved    uint32
	FlagsEx       uint32
}

func fileDialog(save bool, title, defName string) string {
	filter := utf16z("Respaldo de TapBoard (*.json)\x00*.json\x00Todos los archivos\x00*.*\x00\x00")
	buf := make([]uint16, 1024)
	copy(buf, utf16z(defName))
	docs := filepath.Join(os.Getenv("USERPROFILE"), "Documents")
	ofn := openFileName{
		Owner:      hwnd,
		Filter:     &filter[0],
		File:       &buf[0],
		MaxFile:    uint32(len(buf)),
		InitialDir: wstr(docs),
		Title:      wstr(title),
		DefExt:     wstr("json"),
		Flags:      0x80000 | 0x8 | 0x800, // EXPLORER | NOCHANGEDIR | PATHMUSTEXIST
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	proc := pGetOpenFileNameW
	if save {
		ofn.Flags |= 0x2 // OVERWRITEPROMPT
		proc = pGetSaveFileNameW
	} else {
		ofn.Flags |= 0x1000 // FILEMUSTEXIST
	}
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func utf16z(s string) []uint16 {
	return append(utf16.Encode([]rune(s)), 0)
}

func settingsNotice(msg string) {
	setMsg, setMsgUntil = msg, time.Now().Add(4*time.Second)
	invalidate(hwnd)
}

func exportConfig() {
	name := "TapBoard-respaldo-" + time.Now().Format("2006-01-02") + ".json"
	path := fileDialog(true, "Guardar respaldo", name)
	if path == "" {
		return
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		settingsNotice("No se pudo guardar el respaldo")
		return
	}
	settingsNotice("Respaldo guardado: " + filepath.Base(path))
}

func importConfig() {
	path := fileDialog(false, "Abrir respaldo", "")
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		settingsNotice("No se pudo leer el archivo")
		return
	}
	nc := defaultConfig()
	nc.Botones = nil
	nc.MainButtons = nil
	if err := json.Unmarshal(migrateConfig(b), &nc); err != nil || !strings.Contains(string(b), "monitor") {
		settingsNotice("Ese archivo no es un respaldo válido")
		return
	}
	cfg = nc
	saveConfig(cfg)
	cfg = loadConfig()
	if page >= numPages() {
		page = numPages() - 1
	}
	resetWhatsApp()
	placeWindow()
	settingsNotice("Respaldo cargado")
	startAnim()
}

// fileDialogAny abre el diálogo de Windows para elegir cualquier archivo.
func fileDialogAny(save bool, title string) string {
	filter := utf16z("Todos los archivos\x00*.*\x00\x00")
	buf := make([]uint16, 1024)
	ofn := openFileName{
		Owner:   hwnd,
		Filter:  &filter[0],
		File:    &buf[0],
		MaxFile: uint32(len(buf)),
		Title:   wstr(title),
		Flags:   0x80000 | 0x8 | 0x800 | 0x1000,
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
