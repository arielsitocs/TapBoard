//go:build windows

package main

// Páginas: 1 = principal, 2 = botones, 3 = monitor del PC, 4+ = botones extra.

import (
	"time"
)

const (
	pageMonitor = 2
	pageMixer   = 3
)

// Botones configurables de la página 1 (fila de abajo).
var mainIDs = []int{tCamera, tFolder, tDesktop, tMoon, tPower}

func mainIndex(id int) int {
	for i, m := range mainIDs {
		if m == id {
			return i
		}
	}
	return -1
}

func isSlotID(id int) bool { return (id >= 1000 && id < 2000) || mainIndex(id) >= 0 }

func numPages() int { return 4 + len(cfg.PaginasExtra) }

func numButtonPages() int { return 1 + len(cfg.PaginasExtra) }

// buttonPageOf devuelve el índice de página de botones (0 = la segunda página
// del panel, 1.. = extras) o -1 si la página no es de botones.
func buttonPageOf(p int) int {
	if p == 1 {
		return 0
	}
	if p >= 4 && p-4 < len(cfg.PaginasExtra) {
		return p - 3
	}
	return -1
}

func panelPageOfBP(bp int) int {
	if bp == 0 {
		return 1
	}
	return bp + 3
}

func slotID(bp, s int) int {
	if bp < 0 {
		return mainIDs[s]
	}
	return 1000 + bp*8 + s
}

func slotOf(id int) (int, int) {
	if i := mainIndex(id); i >= 0 {
		return -1, i
	}
	return (id - 1000) / 8, (id - 1000) % 8
}

func slotRect(s int) RectF {
	cols, rows := 4, 2
	bottom := pad*1.2 + 10
	cw := (panelW - pad*float32(cols+1)) / float32(cols)
	ch := (panelH - pad*float32(rows) - bottom) / float32(rows)
	return RectF{pad + float32(s%cols)*(cw+pad), pad + float32(s/cols)*(ch+pad), cw, ch}
}

func rectOf(id int) RectF {
	if id >= 2000 {
		return mixerRect(id)
	}
	if id >= 1000 {
		_, s := slotOf(id)
		return slotRect(s)
	}
	if id >= 0 && id < tCount {
		return tiles[id]
	}
	return RectF{}
}

func emptyPage() []BotonP2 {
	b := make([]BotonP2, 8)
	for i := range b {
		b[i] = BotonP2{Tipo: "action", Accion: "empty"}
	}
	return b
}

// pageSlots devuelve (y asegura) los 8 botones de una página de botones.
func pageSlots(bp int) []BotonP2 {
	if bp < 0 {
		for len(cfg.MainButtons) < 5 {
			cfg.MainButtons = append(cfg.MainButtons, defaultMainButtons()[len(cfg.MainButtons)])
		}
		return cfg.MainButtons
	}
	if bp == 0 {
		for len(cfg.Botones) < 8 {
			cfg.Botones = append(cfg.Botones, defaultBotones()[len(cfg.Botones)])
		}
		return cfg.Botones
	}
	if bp-1 < len(cfg.PaginasExtra) {
		for len(cfg.PaginasExtra[bp-1]) < 8 {
			cfg.PaginasExtra[bp-1] = append(cfg.PaginasExtra[bp-1], BotonP2{Tipo: "action", Accion: "empty"})
		}
		return cfg.PaginasExtra[bp-1]
	}
	return emptyPage()
}

func addButtonPage() {
	cfg.PaginasExtra = append(cfg.PaginasExtra, emptyPage())
	saveConfig(cfg)
	invalidate(hwnd)
}

func removeButtonPage(bp int) {
	if bp < 1 || bp-1 >= len(cfg.PaginasExtra) {
		return
	}
	cfg.PaginasExtra = append(cfg.PaginasExtra[:bp-1], cfg.PaginasExtra[bp:]...)
	if page >= numPages() {
		page = numPages() - 1
	}
	saveConfig(cfg)
	startAnim()
}

func paintButtonPage(c *Canvas, bp int) {
	for s := 0; s < 8; s++ {
		drawSlot(c, bp, s)
	}
}

// ---------------------------------------------------------------- reordenar

var (
	reorderID  = -1
	vswiping   bool
	lastPX     int32
	lastPY     int32
	pressStart time.Time
)

// checkLongPress: mantener presionado un botón de página de botones lo "levanta".
func checkLongPress() {
	if reorderID >= 0 || !isSlotID(pressed) || swiping || vswiping || dragging {
		return
	}
	if time.Since(pressStart) < 480*time.Millisecond {
		return
	}
	dx, dy := lastPX-downX, lastPY-downY
	if dx*dx+dy*dy > int32(panelH*panelH*0.0009) {
		return
	}
	reorderID = pressed
	pressed = -1
	pendingTile = -1
	pageSound()
	startAnim()
}

func finishReorder(x, y int32) {
	src := reorderID
	reorderID = -1
	dst := hitTest(x, y)
	if isSlotID(dst) && dst != src {
		bp, a := slotOf(src)
		bp2, b := slotOf(dst)
		if bp == bp2 {
			sl := pageSlots(bp)
			sl[a], sl[b] = sl[b], sl[a]
			saveConfig(cfg)
			clickSound()
		}
	}
	startAnim()
}

func paintReorderGhost(c *Canvas) {
	if reorderID < 0 {
		return
	}
	bp, s := slotOf(reorderID)
	r := rectOf(reorderID)
	img, cap, col := slotVisual(bp, s)
	w, h := r.W*1.06, r.H*1.06
	x, y := float32(lastPX)-w/2, float32(lastPY)-h/2
	if bp >= 0 {
		sn, m := appCardSnap(r, col, img)
		if sn != nil {
			k := w / r.W
			c.RoundRect(x, y+h*0.05, w, h, min32(w, h)*0.2, 0x55000000)
			c.DrawSnapshotRect(sn, x-float32(m)*k, y-float32(m)*k, float32(sn.w)*k, float32(sn.h)*k)
			padI := h * 0.11
			c.TextWrap(cap, semiFont, min32(w, h)*0.088, semiStyle, 0xF2FFFFFF, RectF{x + padI, y + h*0.56, w - 2*padI, h*0.44 - padI*0.8}, AlignNear, AlignFar)
		}
		return
	}
	col = themeTile(col)
	rad := min32(w, h) * 0.26
	c.RoundRect(x, y+h*0.05, w, h, rad, 0x55000000)
	c.RoundRect(x, y, w, h, rad, lighten(col, 0.1))
	if img != nil {
		is := min32(w, h) * 0.42
		c.DrawImage(img, x+(w-is)/2, y+(h-is)/2, is, is)
	}
}

// nearPage indica si la página p está visible (o entrando con el dedo).
func nearPage(p int) bool {
	if panelW <= 0 || settingsAnim > 0.99 || pickerAnim > 0.99 {
		return false
	}
	d := scrollX - float32(p)*panelW
	return d > -panelW && d < panelW
}

// ---------------------------------------------------------------- instantáneas

var pageSnaps = map[int]*Snapshot{}

// buildSnaps dibuja la página actual y sus vecinas en imágenes, para que el
// deslizamiento solo tenga que copiarlas (mucho más fluido).
func buildSnaps() {
	freeSnaps()
	if panelW <= 0 {
		return
	}
	for p := page - 1; p <= page+1; p++ {
		if p < 0 || p >= numPages() {
			continue
		}
		sn, c := newSnapshot(int(panelW), int(panelH))
		if sn == nil {
			continue
		}
		paintPageContent(c, p)
		c.Close()
		pageSnaps[p] = sn
	}
}

func freeSnaps() {
	for k, s := range pageSnaps {
		s.Free()
		delete(pageSnaps, k)
	}
}

func snapsLive() bool { return len(pageSnaps) > 0 }

func useSnaps() bool { return snapsLive() && (swiping || scrollX != float32(page)*panelW) }

func snapFor(p int) *Snapshot { return pageSnaps[p] }

func paintPageContent(c *Canvas, p int) {
	if p == 0 {
		paintPage1(c)
	} else if p == pageMonitor {
		paintMonitorPage(c)
	} else if p == pageMixer {
		paintMixerPage(c)
	} else if bp := buttonPageOf(p); bp >= 0 {
		paintButtonPage(c, bp)
	}
}
