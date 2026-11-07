//go:build windows

package main

// Menú del engranaje dibujado por nosotros (texto grande y tema oscuro).

import (
	"syscall"
	"unsafe"
)

var (
	pSetMenuInfo      = user32.NewProc("SetMenuInfo")
	pCreateSolidBrush = syscall.NewLazyDLL("gdi32.dll").NewProc("CreateSolidBrush")
	menuBrush         uintptr
)

const (
	MF_OWNERDRAW    = 0x0100
	MF_POPUP        = 0x0010
	WM_MEASUREITEM  = 0x002C
	WM_DRAWITEM     = 0x002B
	ODS_SELECTED    = 0x0001
	ODS_DISABLED    = 0x0004
	ODS_CHECKED     = 0x0008
	menuBG          = 0xFF202124
	menuHover       = 0xFF34373C
	MF_GRAYEDDISABL = 0x0003
)

type ownerItem struct {
	text     string
	sep      bool
	disabled bool
	checked  bool
	submenu  bool
	glyph    rune // ícono de línea a la izquierda (tema Grafito)
	toggle   bool // se dibuja un interruptor en vez de la marca ✓
	danger   bool // en rojo (Salir)
	header   bool // título tenue del menú (versión)
}

var ownerItems []ownerItem

func menuFontPx() float32 {
	f := panelH * 0.038
	if f < 17 {
		f = 17
	}
	return f
}

// Íconos, interruptores y opciones en rojo, según el texto del ítem.
var (
	menuGlyphs = map[string]rune{
		"Salida de audio":                   0xE7F6,
		"Configuración del panel…":          0xE713,
		"Configuración…":                    0xE713,
		"Historial de WhatsApp":             0xE8BD,
		"Abrir config.json":                 0xE8A5,
		"Recargar configuración":            0xE72C,
		"Iniciar con Windows":               0xE770,
		"Mostrar el panel en esta pantalla": 0xE7F4,
		"Esconder el panel":                 0xE7F4,
		"Salir":                             0xE7E8,
	}
	menuToggles = map[string]bool{"Iniciar con Windows": true}
	menuDanger  = map[string]bool{"Salir": true}
)

// menuAppend agrega un ítem dibujado por la app.
func menuAppend(m uintptr, flags uintptr, id uintptr, text string) {
	menuAppendGlyph(m, flags, id, text, menuGlyphs[text])
}

func menuAppendGlyph(m uintptr, flags uintptr, id uintptr, text string, glyph rune) {
	it := ownerItem{
		text:     text,
		sep:      flags&MF_SEPARATOR != 0,
		disabled: flags&MF_GRAYEDDISABL != 0,
		checked:  flags&MF_CHECKED != 0,
		submenu:  flags&MF_POPUP != 0,
		glyph:    glyph,
		toggle:   menuToggles[text],
		danger:   menuDanger[text],
	}
	it.header = it.disabled && len(ownerItems) == 0 && glyph == 0
	ownerItems = append(ownerItems, it)
	pAppendMenuW.Call(m, flags|MF_OWNERDRAW, id, uintptr(len(ownerItems)))
}

var menuBrushGrafito uintptr

func styleMenu(m uintptr) {
	if menuBrush == 0 {
		menuBrush, _, _ = pCreateSolidBrush.Call(0x00242120)          // BGR de menuBG
		menuBrushGrafito, _, _ = pCreateSolidBrush.Call(0x002F2521) // BGR de gSurface
	}
	brush := menuBrush
	if isGrafito() {
		brush = menuBrushGrafito
	}
	type menuInfo struct {
		CbSize, FMask, DwStyle, CyMax uint32
		HbrBack                       uintptr
		DwContextHelpID               uint32
		DwMenuData                    uintptr
	}
	mi := menuInfo{FMask: 0x2 | 0x80000000, HbrBack: brush} // MIM_BACKGROUND | MIM_APPLYTOSUBMENUS
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pSetMenuInfo.Call(m, uintptr(unsafe.Pointer(&mi)))
}

func onMeasureItem(lparam uintptr) {
	type measureItem struct {
		CtlType, CtlID, ItemID, ItemWidth, ItemHeight uint32
		ItemData                                      uintptr
	}
	mi := (*measureItem)(unsafe.Pointer(lparam))
	i := int(mi.ItemData) - 1
	if i < 0 || i >= len(ownerItems) {
		return
	}
	fs := menuFontPx()
	it := ownerItems[i]
	if isGrafito() {
		fs = gMenuFontPx()
		switch {
		case it.sep:
			mi.ItemHeight, mi.ItemWidth = uint32(fs*0.9), 10
		case it.header:
			mi.ItemHeight = uint32(fs * 2.0)
			mi.ItemWidth = uint32(float32(len([]rune(it.text)))*fs*0.5 + fs*3)
		default:
			mi.ItemHeight = uint32(fs * 2.35)
			mi.ItemWidth = uint32(float32(len([]rune(it.text)))*fs*0.54 + fs*6.2)
		}
		return
	}
	if it.sep {
		mi.ItemHeight = uint32(fs * 0.7)
		mi.ItemWidth = 10
		return
	}
	mi.ItemHeight = uint32(fs * 2.1)
	mi.ItemWidth = uint32(float32(len([]rune(it.text)))*fs*0.56 + fs*3.6)
}

func gMenuFontPx() float32 {
	f := panelH * 0.03
	if f < 15 {
		f = 15
	}
	return f
}

// gDrawMenuItem: ítem del menú en el estilo Grafito.
func gDrawMenuItem(c *Canvas, r RectF, it ownerItem, state uint32) {
	fs := gMenuFontPx()
	c.RoundRect(r.X, r.Y, r.W, r.H, 0, gSurface)
	if it.sep {
		y := r.Y + r.H/2
		c.Line(r.X+fs*0.9, y, r.X+r.W-fs*0.9, y, 1, 0xFF333846)
		return
	}
	if it.header {
		c.Text(upper(it.text), semiFont, fs*0.72, semiStyle, gMuted, RectF{r.X + fs * 1.0, r.Y, r.W - fs*2, r.H}, AlignNear, AlignCenter)
		return
	}
	selected := state&ODS_SELECTED != 0 && !it.disabled
	if selected {
		c.RoundRect(r.X+fs*0.35, r.Y+fs*0.12, r.W-fs*0.7, r.H-fs*0.24, fs*0.42, gRaised)
	}
	txt, ic := uint32(gText), uint32(0xFFA3A9B6)
	if selected {
		ic = gText
	}
	if it.disabled {
		txt, ic = gMuted, gMuted
	}
	if it.danger {
		txt, ic = gRed, gRed
	}
	x := r.X + fs*1.0
	checked := it.checked || state&ODS_CHECKED != 0
	g := it.glyph
	if checked && !it.toggle {
		g, ic = 0xE73E, gAccent // marca de elegido (salidas de audio)
	}
	if g != 0 {
		is := fs * 1.05
		gIcon(c, nil, g, x, r.Y+(r.H-is)/2, is, ic)
	}
	tx := x + fs*1.75
	right := r.X + r.W - fs*1.0
	if it.toggle {
		// interruptor a la derecha
		tw, th := fs*1.9, fs*1.1
		sx, sy := right-tw, r.Y+(r.H-th)/2
		bg, knob, kx := uint32(gTrack), uint32(0xFFA3A9B6), sx+th/2
		if checked {
			bg, knob, kx = gAccent, 0xFFFFFFFF, sx+tw-th/2
		}
		c.RoundRect(sx, sy, tw, th, th/2, bg)
		c.Circle(kx, sy+th/2, th*0.36, knob)
		right = sx - fs*0.6
	}
	if it.submenu {
		right -= fs * 0.9 // espacio para la flecha que dibuja Windows
	}
	c.Text(it.text, textFont, fs, FontStyleRegular, txt, RectF{tx, r.Y, right - tx, r.H}, AlignNear, AlignCenter)
}

func onDrawItem(lparam uintptr) {
	type drawItem struct {
		CtlType, CtlID, ItemID, ItemAction, ItemState uint32
		HwndItem                                      uintptr
		HDC                                           uintptr
		RcItem                                        RECT
		ItemData                                      uintptr
	}
	di := (*drawItem)(unsafe.Pointer(lparam))
	i := int(di.ItemData) - 1
	if i < 0 || i >= len(ownerItems) {
		return
	}
	it := ownerItems[i]
	fs := menuFontPx()
	c := newCanvas(di.HDC)
	defer c.Close()
	r := RectF{float32(di.RcItem.L), float32(di.RcItem.T), float32(di.RcItem.R - di.RcItem.L), float32(di.RcItem.B - di.RcItem.T)}
	if isGrafito() {
		gDrawMenuItem(c, r, it, di.ItemState)
		return
	}
	bg, hover, line, check := uint32(menuBG), uint32(menuHover), uint32(0xFF3A3D42), uint32(0xFF4FC3F7)
	if isGrafito() {
		bg, hover, line, check = gSurface, gRaised, 0xFF3A3F48, gAccent
	}
	c.RoundRect(r.X, r.Y, r.W, r.H, 0, bg)
	if it.sep {
		y := r.Y + r.H/2
		c.Line(r.X+fs*0.6, y, r.X+r.W-fs*0.6, y, 1, line)
		return
	}
	selected := di.ItemState&ODS_SELECTED != 0 && !it.disabled
	if selected {
		c.RoundRect(r.X+4, r.Y+2, r.W-8, r.H-4, fs*0.45, hover)
	}
	col := uint32(0xFFF2F2F2)
	if it.disabled {
		col = 0xFF8A8F98
	}
	if it.checked || di.ItemState&ODS_CHECKED != 0 {
		c.Text("", iconFont, fs*0.85, FontStyleRegular, check, RectF{r.X, r.Y, fs * 2.2, r.H}, AlignCenter, AlignCenter)
	}
	c.Text(it.text, textFont, fs, FontStyleRegular, col, RectF{r.X + fs*2.2, r.Y, r.W - fs*3.6, r.H}, AlignNear, AlignCenter)
}
