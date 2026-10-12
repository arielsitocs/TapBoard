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
}

var ownerItems []ownerItem

func menuFontPx() float32 {
	f := panelH * 0.038
	if f < 17 {
		f = 17
	}
	return f
}

// menuAppend agrega un ítem dibujado por la app.
func menuAppend(m uintptr, flags uintptr, id uintptr, text string) {
	it := ownerItem{
		text:     text,
		sep:      flags&MF_SEPARATOR != 0,
		disabled: flags&MF_GRAYEDDISABL != 0,
		checked:  flags&MF_CHECKED != 0,
		submenu:  flags&MF_POPUP != 0,
	}
	ownerItems = append(ownerItems, it)
	pAppendMenuW.Call(m, flags|MF_OWNERDRAW, id, uintptr(len(ownerItems)))
}

func styleMenu(m uintptr) {
	if menuBrush == 0 {
		menuBrush, _, _ = pCreateSolidBrush.Call(0x00242120) // BGR de menuBG
	}
	type menuInfo struct {
		CbSize, FMask, DwStyle, CyMax uint32
		HbrBack                       uintptr
		DwContextHelpID               uint32
		DwMenuData                    uintptr
	}
	mi := menuInfo{FMask: 0x2 | 0x80000000, HbrBack: menuBrush} // MIM_BACKGROUND | MIM_APPLYTOSUBMENUS
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
	if it.sep {
		mi.ItemHeight = uint32(fs * 0.7)
		mi.ItemWidth = 10
		return
	}
	mi.ItemHeight = uint32(fs * 2.1)
	mi.ItemWidth = uint32(float32(len([]rune(it.text)))*fs*0.56 + fs*3.6)
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
	c.RoundRect(r.X, r.Y, r.W, r.H, 0, menuBG)
	if it.sep {
		y := r.Y + r.H/2
		c.Line(r.X+fs*0.6, y, r.X+r.W-fs*0.6, y, 1, 0xFF3A3D42)
		return
	}
	selected := di.ItemState&ODS_SELECTED != 0 && !it.disabled
	if selected {
		c.RoundRect(r.X+4, r.Y+2, r.W-8, r.H-4, fs*0.45, menuHover)
	}
	col := uint32(0xFFF2F2F2)
	if it.disabled {
		col = 0xFF8A8F98
	}
	if it.checked || di.ItemState&ODS_CHECKED != 0 {
		c.Text("", iconFont, fs*0.85, FontStyleRegular, 0xFF4FC3F7, RectF{r.X, r.Y, fs * 2.2, r.H}, AlignCenter, AlignCenter)
	}
	c.Text(it.text, textFont, fs, FontStyleRegular, col, RectF{r.X + fs*2.2, r.Y, r.W - fs*3.6, r.H}, AlignNear, AlignCenter)
}
