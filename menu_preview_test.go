//go:build windows

package main

// Dibuja el menú del engranaje en un PNG para revisarlo sin abrir la app.
// Solo corre si MENU_PREVIEW apunta al archivo de salida.

import (
	"image"
	"image/png"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestMenuPreview(t *testing.T) {
	out := os.Getenv("MENU_PREVIEW")
	if out == "" {
		t.Skip("MENU_PREVIEW no definido")
	}
	if !gdiplusInit() {
		t.Fatal("GDI+")
	}
	pickFonts()
	cfg = defaultConfig()
	cfg.Theme = "grafito"
	panelH = 600
	ownerItems = nil
	add := func(flags uintptr, text string, glyph rune) {
		it := ownerItem{text: text, sep: flags&MF_SEPARATOR != 0, disabled: flags&MF_GRAYEDDISABL != 0,
			checked: flags&MF_CHECKED != 0, submenu: flags&MF_POPUP != 0, glyph: glyph,
			toggle: menuToggles[text], danger: menuDanger[text]}
		it.header = it.disabled && len(ownerItems) == 0 && glyph == 0
		ownerItems = append(ownerItems, it)
	}
	add(MF_STRING|0x3, "TapBoard v"+appVersion, 0)
	add(MF_STRING|0x3, "Ojos Así · Shakira", 0xEC4F)
	add(MF_SEPARATOR, "", 0)
	add(MF_STRING|MF_POPUP, "Salida de audio", menuGlyphs["Salida de audio"])
	add(MF_STRING, "Configuración del panel…", menuGlyphs["Configuración del panel…"])
	add(MF_STRING, "Historial de WhatsApp", menuGlyphs["Historial de WhatsApp"])
	add(MF_STRING, "Abrir config.json", menuGlyphs["Abrir config.json"])
	add(MF_STRING, "Recargar configuración", menuGlyphs["Recargar configuración"])
	add(MF_STRING|MF_CHECKED, "Iniciar con Windows", menuGlyphs["Iniciar con Windows"])
	add(MF_SEPARATOR, "", 0)
	add(MF_STRING, "Salir", menuGlyphs["Salir"])

	fs := gMenuFontPx()
	w := 420
	var hs []int
	total := 0
	for _, it := range ownerItems {
		h := int(fs * 2.35)
		if it.sep {
			h = int(fs * 0.9)
		} else if it.header {
			h = int(fs * 2.0)
		}
		hs = append(hs, h)
		total += h
	}
	total += 8
	gdi := syscall.NewLazyDLL("gdi32.dll")
	createDIB := gdi.NewProc("CreateDIBSection")
	type bih struct {
		Size                 uint32
		Width, Height        int32
		Planes, Bits         uint16
		Compr, SizeImg       uint32
		XPels, YPels         int32
		ClrUsed, ClrImportnt uint32
	}
	bi := bih{Width: int32(w), Height: -int32(total), Planes: 1, Bits: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	dc, _, _ := pCreateCompatibleDC.Call(0)
	bmp, _, _ := createDIB.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	pSelectObject.Call(dc, bmp)
	c := newCanvas(dc)
	c.RoundRect(0, 0, float32(w), float32(total), 0, gSurface)
	y := 4
	for i, it := range ownerItems {
		state := uint32(0)
		if i == 4 {
			state = ODS_SELECTED // una opción con el dedo encima
		}
		if it.checked {
			state |= ODS_CHECKED
		}
		gDrawMenuItem(c, RectF{4, float32(y), float32(w - 8), float32(hs[i])}, it, state)
		y += hs[i]
	}
	c.Close()
	img := image.NewRGBA(image.Rect(0, 0, w, total))
	src := unsafe.Slice((*byte)(bits), w*total*4)
	for i := 0; i < w*total; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = src[i*4+2], src[i*4+1], src[i*4], 255
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
