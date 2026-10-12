//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------- estado

const (
	// Página 1
	tSpeaker = iota
	tVolume
	tMic
	tPrev
	tPlay
	tNext
	tHeadphones
	tCamera
	tFolder
	tDesktop // cerrar app
	tMoon
	tPower
	tGear
	tCount
)

type tileDef struct {
	page, col, row, cspan int
}

// Cuadrícula de cada página: columnas, filas.
var pageGrid = [][2]int{{5, 3}, {4, 2}}

var tileDefs = [tCount]tileDef{
	tSpeaker:    {0, 0, 0, 1},
	tVolume:     {0, 1, 0, 4},
	tMic:        {0, 0, 1, 1},
	tPrev:       {0, 1, 1, 1},
	tPlay:       {0, 2, 1, 1},
	tNext:       {0, 3, 1, 1},
	tHeadphones: {0, 4, 1, 1},
	tCamera:     {0, 0, 2, 1},
	tFolder:     {0, 1, 2, 1},
	tDesktop:    {0, 2, 2, 1},
	tMoon:       {0, 3, 2, 1},
	tPower:      {0, 4, 2, 1},
	tGear:       {0, 0, 0, 0},
}

const (
	colBG      = 0xFF141414
	colTile    = 0xFF3A3F47
	colCard    = 0xFF1F1F1F
	colPurple  = 0xFF4A3466
	colTeal    = 0xFF1F5B5D
	colBrown   = 0xFF6D5328
	colGreen   = 0xFF2C4A3E
	colCloseBG = 0xFF4A3030
	colNavy    = 0xFF233650
	colRed     = 0xFF6E2A33
	colRedOn   = 0xFF8C2D3A
	colWhite   = 0xFFFFFFFF
	colGrey    = 0xFFB4B8BF
	colDim     = 0xFF6F747C
	colBlue    = 0xFF2196F3
	colTrack   = 0xFF2B2B2B
)

type flashMsg struct {
	text  string
	until time.Time
}

type ripple struct {
	tile int
	x, y float32
	t0   time.Time
}

const rippleDur = 450 * time.Millisecond

var (
	hwnd      uintptr
	cfg       Config
	audio     *Audio
	aud       AudioState
	iconFont  = "Segoe Fluent Icons"
	textFont  = "Segoe UI"
	semiFont  = "Segoe UI Semibold"
	semiStyle = FontStyleRegular
	tiles     [tCount]RectF
	pad       float32
	panelRect RECT
	panelW    float32
	panelH    float32

	lastFg     uintptr
	lastCursor POINT
	haveCursor bool

	pressed             = -1
	activePointer int64 = math.MinInt64
	dragging      bool

	// páginas y deslizamiento
	page      int
	scrollX   float32 // desplazamiento actual en píxeles (page*w cuando está quieto)
	swiping   bool
	downX     int32
	downY     int32
	downTile  = -1
	swipeBase float32

	// animaciones
	pressAnim = map[int]float32{}
	ripples   []ripple
	animOn    bool

	playPaused  bool
	closeTarget uintptr

	pendingTile  = -1
	pendingUntil time.Time
	flashes      = map[int]flashMsg{}
	reshowAt     []time.Time

	tickCount int
	ownPid    uint32
	demo      bool
)

// ---------------------------------------------------------------- layout

func layout(w, h float32) {
	panelW, panelH = w, h
	pad = h * 0.015 // espacio entre botones
	if p := w * 0.0095; p < pad {
		pad = p
	}
	if pad < 4 {
		pad = 4
	}
	bottom := pad*1.2 + 10 // espacio para los puntitos de página
	for i, d := range tileDefs {
		if i == tGear {
			continue
		}
		cols, rows := pageGrid[d.page][0], pageGrid[d.page][1]
		cw := (w - pad*float32(cols+1)) / float32(cols)
		ch := (h - pad*float32(rows) - bottom) / float32(rows)
		x := pad + float32(d.col)*(cw+pad)
		y := pad + float32(d.row)*(ch+pad)
		tw := cw*float32(d.cspan) + pad*float32(d.cspan-1)
		tiles[i] = RectF{x, y, tw, ch}
	}
	v := tiles[tVolume]
	g := v.H * 0.30
	tiles[tGear] = RectF{v.X + v.W - g - v.H*0.05, v.Y + v.H*0.05, g, g}
	if !swiping && !animOn {
		scrollX = float32(page) * w
	}
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func inRect(r RectF, x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func hitTest(x, y int32) int {
	fx, fy := float32(x), float32(y)
	if page == 0 && inRect(tiles[tGear], fx, fy) {
		return tGear
	}
	if page == 0 {
		for i := 0; i < tCount; i++ {
			if i != tGear && inRect(tiles[i], fx, fy) {
				return i
			}
		}
	}
	if page == pageMixer {
		return mixerHit(x, y)
	}
	if bp := buttonPageOf(page); bp >= 0 {
		for sl := 0; sl < 8; sl++ {
			if inRect(slotRect(sl), fx, fy) {
				return slotID(bp, sl)
			}
		}
	}
	return -1
}

func lighten(c uint32, amt float32) uint32 {
	a := c >> 24
	r := float32((c >> 16) & 0xFF)
	g := float32((c >> 8) & 0xFF)
	b := float32(c & 0xFF)
	r += (255 - r) * amt
	g += (255 - g) * amt
	b += (255 - b) * amt
	return a<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

func darken(c uint32, amt float32) uint32 {
	a := c >> 24
	r := float32((c>>16)&0xFF) * (1 - amt)
	g := float32((c>>8)&0xFF) * (1 - amt)
	b := float32(c&0xFF) * (1 - amt)
	return a<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// ---------------------------------------------------------------- dibujo

func radius(r RectF) float32 { return min32(r.W, r.H) * 0.17 }

// visualRect aplica la animación de "hundirse" al presionar.
func visualRect(i int) RectF {
	r := rectOf(i)
	s := 1 - 0.06*pressAnim[i] + bounceScale(i)
	nw, nh := r.W*s, r.H*s
	return RectF{r.X + (r.W-nw)/2, r.Y + (r.H-nh)/2, nw, nh}
}

func tileBG(c *Canvas, i int, col uint32) RectF {
	r := visualRect(i)
	col = themeTile(col)
	r0 := rectOf(i)
	if pa := pressAnim[i]; pa > 0.01 {
		// presionado: se dibuja la tarjeta en caché un poco más chica + brillo
		m := 3 + int(r0.H*0.06)
		iw, ih := ri(r0.W), ri(r0.H)
		key := "glass|" + itoa(iw) + "|" + itoa(ih) + "|" + itoa(ri(radius(r0))) + "|" + hex32(col)
		glassCard(c, -9999, -9999, r0.W, r0.H, radius(r0), col) // asegura la caché
		if e, ok := rcache[key]; ok {
			k := r.W / float32(iw)
			c.DrawSnapshotRect(e.sn, r.X-float32(m)*k, r.Y-float32(m)*k, float32(iw+2*m)*k, float32(ih+2*m)*k)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, radius(r), uint32(28*pa)<<24|0xFFFFFF)
	} else {
		glassCard(c, r.X, r.Y, r.W, r.H, radius(r), col)
	}
	drawRipples(c, i, r, radius(r))
	return r
}

func drawRipples(c *Canvas, i int, r RectF, rad float32) {
	now := time.Now()
	clipped := false
	for _, rp := range ripples {
		if rp.tile != i {
			continue
		}
		t := float32(now.Sub(rp.t0)) / float32(rippleDur)
		if t < 0 || t >= 1 {
			continue
		}
		if !clipped {
			c.ClipRoundRect(r.X, r.Y, r.W, r.H, rad)
			clipped = true
		}
		e := 1 - (1-t)*(1-t)*(1-t) // ease-out
		maxR := float32(math.Hypot(float64(r.W), float64(r.H)))
		a := uint32(70 * (1 - t))
		c.Circle(rp.x, rp.y, maxR*e, a<<24|0xFFFFFF)
	}
	if clipped {
		c.ResetClip()
	}
}

// drawIconTile dibuja un botón con un ícono (emoji 3D) centrado.
func drawIconTile(c *Canvas, i int, col uint32, icon string, caption string) {
	drawImgTile(c, i, col, icons[icon], caption)
}

func drawImgTile(c *Canvas, i int, col uint32, img *Image, caption string) {
	if i >= 1000 && i < 2000 {
		drawAppCard(c, i, col, img, caption)
		return
	}
	drawMinimalTile(c, i, col, img, caption)
}

func flashText(i int) string {
	if f, ok := flashes[i]; ok && time.Now().Before(f.until) {
		return f.text
	}
	return ""
}

func confirmText(i int) string {
	if pendingTile == i && time.Now().Before(pendingUntil) {
		return "Toca otra vez"
	}
	return ""
}

func paint(c *Canvas) {
	frameNo++
	defer pruneCache()
	// el fondo ya se copió con GDI (BitBlt) en onPaint
	for p := 0; p < numPages(); p++ {
		if settingsAnim >= 0.999 || pickerAnim >= 0.999 || idleAnim >= 0.999 {
			break
		}
		off := float32(p)*panelW - scrollX
		if off <= -panelW || off >= panelW {
			continue
		}
		if useSnaps() {
			if sn := snapFor(p); sn != nil {
				c.ResetTransform()
				c.DrawSnapshot(sn, int(math.Round(float64(off))), 0)
				if d := float32(math.Abs(float64(off))) / panelW; d > 0.01 {
					c.RoundRect(off-2, -2, panelW+4, panelH+4, 0, uint32(150*d)<<24|themeBG()&0xFFFFFF)
				}
				continue
			}
		}
		c.Translate(off, 0)
		if p == 0 {
			paintPage1(c)
		} else if p == pageMonitor {
			paintMonitorPage(c)
		} else if p == pageMixer {
			paintMixerPage(c)
		} else if bp := buttonPageOf(p); bp >= 0 {
			paintButtonPage(c, bp)
		}
		// profundidad al deslizar: la página que se va se oscurece
		if d := float32(math.Abs(float64(off))) / panelW; d > 0.01 {
			c.RoundRect(-2, -2, panelW+4, panelH+4, 0, uint32(150*d)<<24|themeBG()&0xFFFFFF)
		}
	}
	c.ResetTransform()
	paintReorderGhost(c)
	paintDots(c)
	paintToast(c)
	paintHistory(c)
	paintIdle(c)
	paintSettingsPanel(c)
	paintPicker(c)
	paintNotif(c)
	paintWhatsNew(c)
}

func paintDots(c *Canvas) {
	r := panelH / 600 * 3.4
	gap := r * 3.0
	y := panelH - (pad*1.2+10)/2
	pos := scrollX / panelW
	n := numPages()
	// ancho total: puntos + una pastilla
	pill := r * 5.0
	total := float32(n-1)*gap + pill
	x := panelW/2 - total/2
	for p := 0; p < n; p++ {
		d := float32(math.Abs(float64(pos - float32(p))))
		if d > 1 {
			d = 1
		}
		w := r*2 + (pill-r*2)*(1-d)
		a := uint32(255 - 165*d)
		c.RoundRect(x, y-r, w, r*2, r, a<<24|0xFFFFFF)
		x += w + gap - r*2
	}
}

func paintPage1(c *Canvas) {
	// Parlante
	if aud.Muted {
		drawIconTile(c, tSpeaker, colRedOn, "muted_speaker", "")
	} else {
		drawIconTile(c, tSpeaker, colTile, "speaker_high_volume", "")
	}

	tv := time.Now()
	drawVolume(c)
	dbgAdd("vol", tv)

	// Micrófono (rojo y tachado cuando está silenciado)
	if aud.MicMuted {
		drawIconTile(c, tMic, colRedOn, "studio_microphone", "")
		r := visualRect(tMic)
		s := min32(r.W, r.H) * 0.42
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		c.Line(cx-s*0.55, cy-s*0.55, cx+s*0.55, cy+s*0.55, s*0.13, 0xFF1A1A1A)
		c.Line(cx-s*0.55, cy-s*0.55, cx+s*0.55, cy+s*0.55, s*0.07, 0xFFFF5A5A)
	} else {
		drawIconTile(c, tMic, colTile, "studio_microphone", "")
		// punto verde: el micrófono está abierto
		r := visualRect(tMic)
		c.Circle(r.X+r.W-r.H*0.13, r.Y+r.H*0.13, r.H*0.04, 0xFF4CD964)
	}

	// Multimedia
	drawIconTile(c, tPrev, colTile, "last_track_button", "")
	if playPaused {
		drawIconTile(c, tPlay, colRedOn, "play_or_pause_button", "")
	} else {
		drawIconTile(c, tPlay, colTile, "play_or_pause_button", "")
	}
	drawIconTile(c, tNext, colTile, "next_track_button", "")

	// Audífonos: siempre muestra la salida actual
	if hcap := flashText(tHeadphones); hcap != "" {
		drawIconTile(c, tHeadphones, colPurple, "headphone", hcap)
	} else {
		// Nombre de la salida actual: pequeño y tenue, sin robar atención.
		r := visualRect(tHeadphones)
		drawIconTile(c, tHeadphones, colPurple, "headphone", "")
		base := min32(r.W, r.H)
		tr := RectF{r.X + r.W*0.1, r.Y + r.H*0.76, r.W * 0.8, r.H * 0.16}
		c.Text(ShortName(aud.DeviceName), semiFont, base*0.078, semiStyle, 0xD0FFFFFF, RectF{r.X + r.W*0.05, tr.Y, r.W * 0.9, tr.H}, AlignCenter, AlignCenter)
	}

	for i := 0; i < len(mainIDs); i++ {
		drawSlot(c, -1, i)
	}
}

func volTrack() (x0, x1, y float32) {
	r := tiles[tVolume]
	m := r.H * 0.12
	if mediaShowing() {
		// franja propia abajo: [parlante] ——barra—— [36 %]
		sb := volStrip()
		return sb.X + sb.H*1.15, sb.X + sb.W - sb.H*1.9, sb.Y + sb.H/2
	}
	return r.X + m, r.X + r.W - m, r.Y + r.H*0.64
}

// volStrip es la franja inferior del volumen cuando hay música.
func volStrip() RectF { return volStripIn(tiles[tVolume]) }

func volStripIn(r RectF) RectF {
	m := r.H * 0.07
	h := r.H * 0.26
	return RectF{r.X + m, r.Y + r.H - m - h, r.W - 2*m, h}
}

func drawVolume(c *Canvas) {
	r := tiles[tVolume]
	volCardBG(c, r)
	m := r.H * 0.12
	fs := r.H * 0.12
	g0 := tiles[tGear]
	if mediaShowing() {
		drawMediaHeader(c, r, g0.X-r.H*0.05)
	} else if cfg.MostrarSpotify && spot.Running && spotSong != "" {
		is := fs * 1.9
		c.DrawImage(icons["musical_note"], r.X+m*0.8, r.Y+m*0.6, is, is)
		tx := r.X + m*0.8 + is + m*0.4
		tw := r.W*0.68 - (tx - r.X)
		songCol, sub := uint32(colWhite), spotArtist
		if !spot.Playing {
			songCol = colGrey
			sub = "En pausa · " + spotArtist
		}
		c.Text(spotSong, semiFont, fs*1.0, semiStyle, songCol, RectF{tx, r.Y + m*0.45, tw, fs * 1.45}, AlignNear, AlignCenter)
		c.Text(sub, textFont, fs*0.82, FontStyleRegular, colDim, RectF{tx, r.Y + m*0.45 + fs*1.35, tw, fs * 1.3}, AlignNear, AlignCenter)
	} else {
		c.Text("Volumen", textFont, fs, FontStyleRegular, colGrey,
			RectF{r.X + m, r.Y + m*0.55, r.W * 0.5, fs * 1.7}, AlignNear, AlignCenter)
	}
	pct := fmt.Sprintf("%d %%", int(math.Round(float64(aud.Volume)*100)))
	if aud.Muted {
		pct = "Silencio"
	}
	g := tiles[tGear]
	if mediaShowing() {
		sb := volStrip()
		c.Text(pct, semiFont, sb.H*0.36, semiStyle, colWhite,
			RectF{sb.X + sb.W - sb.H*1.75, sb.Y, sb.H * 1.5, sb.H}, AlignFar, AlignCenter)
	} else {
		c.Text(pct, semiFont, fs*1.08, semiStyle, colWhite,
			RectF{r.X + r.W*0.4, r.Y + m*0.55, g.X - (r.X + r.W*0.4) - m*0.15, fs * 1.7}, AlignFar, AlignCenter)
	}
	gc := uint32(colDim)
	if pressed == tGear {
		gc = colWhite
	}
	c.Text("", iconFont, g.H*0.66, FontStyleRegular, gc, g, AlignCenter, AlignCenter)

	x0, x1, y := volTrack()
	th := r.H * 0.085
	if mediaShowing() {
		th = r.H * 0.045 // más discreta cuando hay música
	}
	if !mediaShowing() {
		c.RoundRect(x0, y-th/2, x1-x0, th, th/2, colTrack)
	}
	fillA, fillB := uint32(0xFF1E88E5), uint32(0xFF4FC3F7)
	if mediaShowing() && mediaAccent != colBlue {
		fillA, fillB = darken(mediaAccent, 0.25), mediaAccent
	}
	if aud.Muted {
		fillA, fillB = colDim, colDim
	}
	fx := x0 + (x1-x0)*aud.Volume
	if fx-x0 > 1 {
		c.RoundRectGradH(x0, y-th/2, fx-x0, th, th/2, fillA, fillB)
	}
	kr := r.H * 0.12
	if mediaShowing() {
		kr = r.H * 0.075
	}
	if pressed == tVolume {
		kr *= 1.15
		c.Circle(fx, y, kr*1.7, 0x332196F3) // halo al arrastrar
	}
	c.Circle(fx, y+kr*0.06, kr*1.04, 0x40000000) // sombra suave
	c.Circle(fx, y, kr, colWhite)
}

var (
	debugPaint               = os.Getenv("PANEL_DEBUG") != ""
	paintTimes               []time.Duration
	backDC, backBmp, backOld uintptr
	backW, backH             int32
)

func onPaint() {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.R-rc.L, rc.B-rc.T
	if w > 0 && h > 0 {
		if backDC == 0 || backW != w || backH != h {
			if backDC != 0 {
				pSelectObject.Call(backDC, backOld)
				pDeleteObject.Call(backBmp)
				pDeleteDC.Call(backDC)
			}
			backDC, _, _ = pCreateCompatibleDC.Call(hdc)
			backBmp, _, _ = pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
			backOld, _, _ = pSelectObject.Call(backDC, backBmp)
			backW, backH = w, h
			freeSnaps()
		}
		layout(float32(w), float32(h))
		t0 := time.Now()
		blitBackground(hdc, w, h)
		c := newCanvas(backDC)
		paint(c)
		c.Close()
		if debugPaint {
			paintTimes = append(paintTimes, time.Since(t0))
			if len(paintTimes) >= 60 {
				var sum time.Duration
				for _, d := range paintTimes {
					sum += d
				}
				f, _ := os.OpenFile("C:\\paint_debug.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				fmt.Fprintf(f, "page %d avg %v bg %v vol %v\n", page, sum/time.Duration(len(paintTimes)), dbgSum["bg"]/60, dbgSum["vol"]/60)
				dbgSum = map[string]time.Duration{}
				f.Close()
				paintTimes = nil
			}
		}
		pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), backDC, 0, 0, SRCCOPY)
	}
	pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}

// ---------------------------------------------------------------- animación

const animTimer = 2

func startAnim() {
	if !animOn {
		animOn = true
		pSetTimer.Call(hwnd, animTimer, 15, 0)
	}
}

func onAnim() {
	busy := false
	if pressed >= 0 {
		if _, ok := pressAnim[pressed]; !ok {
			pressAnim[pressed] = 0
		}
	}
	for i := range pressAnim {
		target := float32(0)
		if pressed == i && !swiping && i != tVolume {
			target = 1
		}
		d := target - pressAnim[i]
		if math.Abs(float64(d)) > 0.01 {
			k := float32(0.35)
			if target == 0 {
				k = 0.18
			}
			pressAnim[i] += d * k
			busy = true
		} else {
			pressAnim[i] = target
			if target == 0 {
				delete(pressAnim, i)
			}
		}
	}
	now := time.Now()
	keep := ripples[:0]
	for _, r := range ripples {
		if now.Sub(r.t0) < rippleDur {
			keep = append(keep, r)
		}
	}
	ripples = keep
	if len(ripples) > 0 {
		busy = true
	}
	if !swiping && panelW > 0 {
		target := float32(page) * panelW
		d := target - scrollX
		if math.Abs(float64(d)) > 0.5 {
			scrollX += d * 0.22
			busy = true
		} else {
			scrollX = target
			if snapsLive() {
				freeSnaps()
			}
		}
	}
	if swiping {
		busy = true
	}
	pt := float32(0)
	if pickerOpen {
		pt = 1
	}
	if d := pt - pickerAnim; math.Abs(float64(d)) > 0.005 {
		pickerAnim += d * 0.22
		busy = true
	} else {
		pickerAnim = pt
	}
	st := float32(0)
	if settingsOpen {
		st = 1
	}
	if d := st - settingsAnim; math.Abs(float64(d)) > 0.005 {
		settingsAnim += d * 0.22
		busy = true
	} else {
		settingsAnim = st
	}
	if animHistory() {
		busy = true
	}
	if animIdle() {
		busy = true
	}
	if bouncing() {
		busy = true
	}
	if animToast() {
		busy = true
	}
	if animWhatsNew() {
		busy = true
	}
	if notifFly != 0 {
		notifDX += float32(notifFly) * panelW * 0.09
		busy = true
		if math.Abs(float64(notifDX)) > float64(panelW) {
			notifFly, notifDX, notifDY = 0, 0, 0
			notifShown, notifAnim = false, 0
		}
	}
	nt := float32(0)
	if notifShown {
		nt = 1
	}
	if d := nt - notifAnim; math.Abs(float64(d)) > 0.01 {
		notifAnim += d * 0.2
		busy = true
	} else {
		notifAnim = nt
	}
	invalidate(hwnd)
	if !busy {
		animOn = false
		pKillTimer.Call(hwnd, animTimer)
	}
}

// ---------------------------------------------------------------- entrada

func setVolumeFromX(x int32) {
	x0, x1, _ := volTrack()
	v := (float32(x) - x0) / (x1 - x0)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	if !demo {
		audio.SetVolume(v)
	}
	aud.Volume = v
	if v > 0 {
		aud.Muted = false
	}
	invalidate(hwnd)
}

func pointerDown(id int64, x, y int32) {
	if activePointer != math.MinInt64 {
		return
	}
	activePointer = id
	if idle || idleAnim > 0.05 {
		wake()
		return
	}
	lastTouch = time.Now()
	swallow = false
	if wnOpen || wnAnim > 0.3 {
		closeWhatsNew()
		swallow = true
		return
	}
	if notifHit(x, y) {
		clickSound()
		notifPressed = true
		notifDX, notifDY, notifFly = 0, 0, 0
		downX, downY = x, y
		downTile, pressed = -1, -1
		invalidate(hwnd)
		return
	}
	if historyOpen {
		historyDown(x, y)
		return
	}
	if pickerOpen {
		pickerDown(x, y)
		return
	}
	if settingsOpen {
		settingsDown(x, y)
		return
	}
	if seekHit(x, y) {
		clickSound()
		seeking = true
		seekFrac = seekFracAt(x)
		downTile, pressed = -1, -1
		invalidate(hwnd)
		return
	}
	t := hitTest(x, y)
	downX, downY = x, y
	downTile = t
	swiping = false
	swipeBase = float32(page) * panelW
	pressed = t
	vswiping = false
	lastPX, lastPY = x, y
	pressStart = time.Now()
	if t >= 0 && t != tVolume {
		clickSound()
	}
	if t == tVolume {
		dragging = true
		setVolumeFromX(x)
	}
	if t >= 2000 && t < 2100 {
		mixDrag = t - 2000
		mixerSetFromX(mixDrag, x)
	}
	startAnim()
}

func pointerMove(id int64, x, y int32) {
	if id != activePointer || swallow {
		return
	}
	if notifPressed {
		notifDX = float32(x - downX)
		notifDY = float32(y - downY)
		if notifDY > 0 {
			notifDY /= 4 // hacia abajo casi no se mueve
		}
		invalidate(hwnd)
		return
	}
	if historyOpen {
		historyMove(x, y)
		return
	}
	if pickerOpen {
		pickerMove(x, y)
		return
	}
	if settingsOpen {
		settingsMove(x, y)
		return
	}
	if seeking {
		seekFrac = seekFracAt(x)
		invalidate(hwnd)
		return
	}
	if dragging {
		setVolumeFromX(x)
		return
	}
	if mixDrag >= 0 {
		mixerSetFromX(mixDrag, x)
		return
	}
	lastPX, lastPY = x, y
	if reorderID >= 0 {
		invalidate(hwnd)
		return
	}
	dx := float32(x - downX)
	dy := float32(y - downY)
	if vswiping {
		invalidate(hwnd)
		return
	}
	if !swiping && dy > panelH*0.05 && math.Abs(float64(dy)) > math.Abs(float64(dx))*1.3 {
		vswiping = true
		pressed = -1
		startAnim()
		return
	}
	if !swiping && math.Abs(float64(dx)) > float64(panelW*0.035) && math.Abs(float64(dx)) > math.Abs(float64(dy)) {
		swiping = true
		pressed = -1
		pressAnim = map[int]float32{}
		buildSnaps()
		startAnim()
	}
	if swiping {
		s := swipeBase - dx
		maxS := float32(numPages()-1) * panelW
		if s < 0 {
			s = s / 3 // resistencia en los bordes
		} else if s > maxS {
			s = maxS + (s-maxS)/3
		}
		scrollX = s
		invalidate(hwnd)
	}
}

func pointerUp(id int64, x, y int32, touch bool) {
	if id != activePointer {
		return
	}
	if swallow {
		swallow = false
		activePointer = math.MinInt64
		if touch {
			restoreFocusAndCursor()
		}
		return
	}
	if notifPressed {
		notifPressed = false
		activePointer = math.MinInt64
		if touch {
			restoreFocusAndCursor()
		}
		moved := math.Abs(float64(notifDX)) > float64(panelW*0.03) || math.Abs(float64(notifDY)) > float64(panelH*0.03)
		switch {
		case math.Abs(float64(notifDX)) > float64(panelW*0.18):
			// deslizar al lado: sale volando hacia ese lado
			notifFly = 1
			if notifDX < 0 {
				notifFly = -1
			}
			notifMore = 0
			pageSound()
		case notifDY < -float32(notifRect().H)*0.35:
			// deslizar hacia arriba: se esconde
			notifShown = false
			notifMore = 0
			notifDX, notifDY = 0, 0
			pageSound()
		case !moved && notifHit(x, y):
			notifShown = false
			notifMore = 0
			notifDX, notifDY = 0, 0
			if isUpdateBanner() {
				startUpdate()
			} else {
				openWhatsApp()
			}
		default:
			notifDX, notifDY = 0, 0 // vuelve a su lugar
		}
		startAnim()
		return
	}
	if historyOpen || pickerOpen || settingsOpen {
		activePointer = math.MinInt64
		if touch {
			restoreFocusAndCursor()
		}
		if historyOpen {
			historyUp(x, y)
		} else if pickerOpen {
			pickerUp(x, y)
		} else {
			settingsUp(x, y)
		}
		return
	}
	if seeking {
		seeking = false
		activePointer = math.MinInt64
		if touch {
			restoreFocusAndCursor()
		}
		seekFrac = seekFracAt(x)
		seekCommit()
		invalidate(hwnd)
		return
	}
	if reorderID >= 0 {
		activePointer = math.MinInt64
		if touch {
			restoreFocusAndCursor()
		}
		finishReorder(x, y)
		return
	}
	if vswiping {
		vswiping = false
		activePointer = math.MinInt64
		pressed = -1
		if touch {
			restoreFocusAndCursor()
		}
		if float32(y-downY) > panelH*0.12 {
			openHistory()
		}
		startAnim()
		return
	}
	if mixDrag >= 0 {
		mixDrag = -1
		activePointer = math.MinInt64
		pressed = -1
		if touch {
			restoreFocusAndCursor()
		}
		startAnim()
		return
	}
	t := hitTest(x, y)
	wasDragging := dragging
	wasSwiping := swiping
	p := pressed
	dragging = false
	swiping = false
	pressed = -1
	activePointer = math.MinInt64
	if touch {
		restoreFocusAndCursor()
	}
	if wasSwiping {
		dx := float32(x - downX)
		if dx < -panelW*0.15 && page < numPages()-1 {
			page++
			pageSound()
		} else if dx > panelW*0.15 && page > 0 {
			page--
			pageSound()
		}
		pendingTile = -1
		startAnim()
		return
	}
	if !wasDragging && t == p && t >= 0 {
		if t != tGear {
			bounces[t] = time.Now()
		}
		startAnim()
		activate(t)
	}
	startAnim()
}

func cancelPointer() {
	swallow = false
	seeking = false
	mixDrag = -1
	vswiping = false
	reorderID = -1
	notifPressed = false
	dragging = false
	swiping = false
	pressed = -1
	activePointer = math.MinInt64
	startAnim()
}

func needsConfirm(t int, enabled bool) bool {
	if !enabled || runningHold { // mantener presionado ya es un gesto deliberado
		return false
	}
	if pendingTile == t && time.Now().Before(pendingUntil) {
		pendingTile = -1
		return false
	}
	pendingTile = t
	pendingUntil = time.Now().Add(3 * time.Second)
	invalidate(hwnd)
	return true
}

func flash(t int, text string) {
	flashes[t] = flashMsg{text, time.Now().Add(1800 * time.Millisecond)}
	invalidate(hwnd)
}

func keyCombo(keys ...uintptr) {
	for _, k := range keys {
		pKeybdEvent.Call(k, 0, 0, 0)
	}
	for i := len(keys) - 1; i >= 0; i-- {
		pKeybdEvent.Call(keys[i], 0, KEYEVENTF_KEYUP, 0)
	}
}

func activate(t int) {
	if t >= 1000 && t < 2000 {
		bp, sl := slotOf(t)
		runSlot(bp, sl)
		invalidate(hwnd)
		return
	}
	if t >= 2000 {
		mixerActivate(t)
		invalidate(hwnd)
		return
	}
	switch t {
	case tSpeaker:
		if demo {
			aud.Muted = !aud.Muted
		} else {
			audio.ToggleMute()
			refreshAudio(true)
		}
		if aud.Muted {
			toast("Sonido silenciado")
		} else {
			toast("Sonido activado")
		}
	case tMic:
		if demo {
			aud.MicMuted = !aud.MicMuted
		} else {
			audio.ToggleMic()
			refreshAudio(true)
		}
		if aud.MicMuted {
			toast("Micrófono silenciado")
		} else {
			toast("Micrófono activado")
		}
	case tPlay:
		sendMediaPlayPause()
		playPaused = !playPaused
		if playPaused {
			toast("En pausa")
		} else {
			toast("Reproduciendo")
		}
	case tPrev:
		sendMediaKey(0xB1) // VK_MEDIA_PREV_TRACK
		playPaused = false
		toast("Canción anterior")
	case tNext:
		sendMediaKey(0xB0) // VK_MEDIA_NEXT_TRACK
		playPaused = false
		toast("Siguiente canción")
	case tHeadphones:
		if !demo {
			faltan := audio.NextDevice(cfg.DispositivosAudio)
			refreshAudio(true)
			if len(faltan) > 0 {
				flash(tHeadphones, "No encontré: "+faltan[0])
				break
			}
		}
		toast("Audio: " + ShortName(aud.DeviceName))
	case tCamera, tFolder, tDesktop, tMoon, tPower:
		runSlot(-1, mainIndex(t))
	case tGear:
		showMenu()
	}
	invalidate(hwnd)
}

func isOurWindow(w uintptr) bool {
	if w == 0 {
		return false
	}
	if w == hwnd {
		return true
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(w, uintptr(unsafe.Pointer(&pid)))
	return pid == ownPid
}

func inPanel(p POINT) bool {
	return p.X >= panelRect.L && p.X < panelRect.R && p.Y >= panelRect.T && p.Y < panelRect.B
}

// restoreFocusAndCursor es una red de seguridad: si por algún motivo
// Windows movió el cursor o activó el panel, se devuelve todo.
func restoreFocusAndCursor() {
	if fg := foregroundWindow(); isOurWindow(fg) && lastFg != 0 {
		if ok, _, _ := pIsWindow.Call(lastFg); ok != 0 {
			pSetForegroundWindow.Call(lastFg)
		}
	}
	if haveCursor && inPanel(getCursorPos()) {
		pSetCursorPos.Call(uintptr(lastCursor.X), uintptr(lastCursor.Y))
	}
}

func refreshAudio(force bool) {
	st := audio.State()
	changed := force ||
		st.Muted != aud.Muted || st.MicMuted != aud.MicMuted || st.MicOK != aud.MicOK ||
		st.DeviceID != aud.DeviceID || math.Abs(float64(st.Volume-aud.Volume)) > 0.004
	if dragging {
		st.Volume = aud.Volume
	}
	aud = st
	if changed {
		invalidate(hwnd)
	}
}

func reshowWindow() {
	after := HWND_NOTOPMOST
	if cfg.SiempreEncima {
		after = HWND_TOPMOST
	}
	pShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	pSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, SWP_NOACTIVATE|SWP_SHOWWINDOW|0x0001|0x0002) // NOSIZE|NOMOVE
}

func onTimer() {
	tickCount++
	if fg := foregroundWindow(); fg != 0 && !isOurWindow(fg) {
		lastFg = fg
	}
	if p := getCursorPos(); !inPanel(p) {
		lastCursor, haveCursor = p, true
	}
	if tickCount%3 == 0 && !demo {
		refreshAudio(false)
	}
	if tickCount%5 == 0 {
		updateSpotify()
	}
	if !demo || os.Getenv("PANEL_DEMO_MEDIA") != "" {
		updateMedia()
	}
	receiveNotifs()
	receiveUpdates()
	receiveWhatsNew()
	if tickCount%5 == 0 {
		checkAutoPage()
	}
	checkLongPress()
	checkIdle()
	if idle && tickCount%10 == 0 {
		invalidate(hwnd)
	}
	if nearPage(pageMixer) {
		if time.Since(mixLast) > 600*time.Millisecond {
			mixLast = time.Now()
			refreshMixer()
		} else if !demo {
			updatePeaks()
		} else {
			demoMixer()
		}
		invalidate(hwnd)
	}
	if tickCount%10 == 0 && (page == pageMonitor || math.Abs(float64(scrollX-float32(pageMonitor)*panelW)) < float64(panelW)) {
		invalidate(hwnd)
	}
	if notifShown && time.Now().After(notifUntil) && !notifPressed {
		notifShown = false
		notifMore = 0
		startAnim()
	}
	if demo && tickCount == 30 {
		addHistory([]Notif{{Title: "Mamá", Body: "Hola"}, {Title: "Mamá", Body: "¿Vienes a comer? Hice tallarines"}})
		notifCh <- []Notif{{Title: "Mamá", Body: "Hola"}, {Title: "Mamá", Body: "¿Vienes a comer? Hice tallarines"}}
	}
	now := time.Now()
	if len(reshowAt) > 0 && now.After(reshowAt[0]) {
		reshowAt = reshowAt[1:]
		reshowWindow()
	}
	if pendingTile >= 0 && now.After(pendingUntil) {
		pendingTile = -1
		invalidate(hwnd)
	}
	for k, f := range flashes {
		if now.After(f.until) {
			delete(flashes, k)
			invalidate(hwnd)
		}
	}
}

// ---------------------------------------------------------------- menú

const (
	cmdOpenConfig    = 1
	cmdReload        = 2
	cmdAutostart     = 3
	cmdExit          = 4
	cmdDevBase       = 100
	cmdPanelSettings = 5
	cmdHistory       = 6
)

func showMenu() {
	ownerItems = nil
	m, _, _ := pCreatePopupMenu.Call()
	styleMenu(m)
	menuAppend(m, MF_STRING|0x0003, 0, "TapBoard v"+appVersion) // deshabilitado
	sp := "Spotify: " + spotifyLastTitle
	if r := []rune(sp); len(r) > 60 {
		sp = string(r[:59]) + "…"
	}
	menuAppend(m, MF_STRING|0x0003, 0, sp)
	menuAppend(m, MF_SEPARATOR, 0, "")
	var devs []AudioDevice
	if !demo {
		devs = audio.RenderDevices()
	}
	if len(devs) > 0 {
		sub, _, _ := pCreatePopupMenu.Call()
		cur := aud.DeviceID
		for i, d := range devs {
			f := uintptr(MF_STRING)
			if d.ID == cur {
				f |= MF_CHECKED
			}
			menuAppend(sub, f, uintptr(cmdDevBase+i), d.Name)
		}
		menuAppend(m, MF_STRING|MF_POPUP, sub, "Salida de audio")
	}
	menuAppend(m, MF_STRING, cmdPanelSettings, "Configuración del panel…")
	menuAppend(m, MF_STRING, cmdHistory, "Historial de WhatsApp")
	menuAppend(m, MF_STRING, cmdOpenConfig, "Abrir config.json")
	menuAppend(m, MF_STRING, cmdReload, "Recargar configuración")
	flags := uintptr(MF_STRING)
	if autostartEnabled() {
		flags |= MF_CHECKED
	}
	menuAppend(m, flags, cmdAutostart, "Iniciar con Windows")
	menuAppend(m, MF_SEPARATOR, 0, "")
	menuAppend(m, MF_STRING, cmdExit, "Salir")

	g := tiles[tGear]
	pt := POINT{int32(g.X+g.W) + panelRect.L, int32(g.Y+g.H) + panelRect.T}
	pSetForegroundWindow.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, TPM_RETURNCMD|TPM_NONOTIFY|TPM_RIGHTALIGN,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, WM_NULL, 0, 0)
	pDestroyMenu.Call(m)

	switch cmd {
	case cmdOpenConfig:
		if _, err := os.Stat(configPath()); err != nil {
			saveConfig(cfg)
		}
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("notepad.exe"), wstrPtr(`"`+configPath()+`"`), 0, SW_SHOWNORMAL)
		return
	case cmdReload:
		cfg = loadConfig()
		placeWindow()
	case cmdAutostart:
		exe, _ := os.Executable()
		setAutostart(!autostartEnabled(), exe)
	case cmdExit:
		pPostQuitMessage.Call(0)
		return
	case cmdPanelSettings:
		openSettings()
	case cmdHistory:
		openHistory()
	default:
		if i := int(cmd) - cmdDevBase; i >= 0 && i < len(devs) {
			setDefaultDevice(devs[i].ID)
			refreshAudio(true)
			toast("Audio: " + ShortName(devs[i].Name))
			invalidate(hwnd)
		}
	}
	if lastFg != 0 {
		pSetForegroundWindow.Call(lastFg)
	}
}

// ---------------------------------------------------------------- ventana

type monitor struct {
	rect RECT
	work RECT
	area int64
}

var monitorsFound []monitor

var enumMonitorsCB = syscall.NewCallback(func(hmon, hdc, lprc, lparam uintptr) uintptr {
	var mi MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	r := mi.RcMonitor
	monitorsFound = append(monitorsFound, monitor{r, mi.RcWork, int64(r.R-r.L) * int64(r.B-r.T)})
	return 1
})

func pickMonitor() RECT {
	monitorsFound = nil
	pEnumDisplayMonitors.Call(0, 0, enumMonitorsCB, 0)
	if len(monitorsFound) == 0 {
		return RECT{0, 0, 800, 480}
	}
	if cfg.Monitor >= 1 && cfg.Monitor <= len(monitorsFound) {
		return monitorsFound[cfg.Monitor-1].rect
	}
	best := monitorsFound[0]
	for _, m := range monitorsFound[1:] {
		if m.area < best.area {
			best = m
		}
	}
	return best.rect
}

func placeWindow() {
	panelRect = pickMonitor()
	after := HWND_NOTOPMOST
	if cfg.SiempreEncima {
		after = HWND_TOPMOST
	}
	pSetWindowPos.Call(hwnd, after, uintptr(panelRect.L), uintptr(panelRect.T),
		uintptr(panelRect.R-panelRect.L), uintptr(panelRect.B-panelRect.T), SWP_NOACTIVATE|SWP_SHOWWINDOW)
	invalidate(hwnd)
}

func isPromotedFromTouch() bool {
	extra, _, _ := pGetMessageExtraInfo.Call()
	return extra&0xFFFFFF00 == 0xFF515700
}

func pointerClient(lparam uintptr) (int32, int32) {
	p := POINT{xParam(lparam), yParam(lparam)}
	pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y
}

func wndProc(h, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case WM_MOUSEACTIVATE:
		return MA_NOACTIVATE
	case WM_POINTERACTIVATE:
		return PA_NOACTIVATE

	// Toque: se procesa aquí y NO se pasa a DefWindowProc, así Windows
	// no lo convierte en clic de mouse (el cursor no se mueve).
	case WM_POINTERDOWN:
		x, y := pointerClient(lparam)
		pointerDown(int64(loword(wparam)), x, y)
		return 0
	case WM_POINTERUPDATE:
		x, y := pointerClient(lparam)
		pointerMove(int64(loword(wparam)), x, y)
		return 0
	case WM_POINTERUP:
		x, y := pointerClient(lparam)
		pointerUp(int64(loword(wparam)), x, y, true)
		return 0
	case WM_POINTERCAPTURECHANGED:
		if int64(loword(wparam)) == activePointer {
			cancelPointer()
		}
		return 0
	case WM_POINTERENTER, WM_POINTERLEAVE:
		return 0
	case WM_TABLET_QUERYSYSTEMGESTURESTATUS:
		return 0x00010019

	// Mouse normal
	case WM_LBUTTONDOWN:
		if isPromotedFromTouch() {
			return 0
		}
		pSetCapture.Call(h)
		pointerDown(-1, xParam(lparam), yParam(lparam))
		return 0
	case WM_MOUSEMOVE:
		if !isPromotedFromTouch() {
			pointerMove(-1, xParam(lparam), yParam(lparam))
		}
		return 0
	case WM_LBUTTONUP:
		if isPromotedFromTouch() {
			return 0
		}
		pReleaseCapture.Call()
		pointerUp(-1, xParam(lparam), yParam(lparam), false)
		return 0
	case 0x020A: // WM_MOUSEWHEEL
		if pickerOpen {
			pickerScroll -= float32(int16(wparam>>16)) / 120 * 80 * panelSetScale()
			clampPicker()
			invalidate(h)
		} else if settingsOpen {
			setScroll -= float32(int16(wparam>>16)) / 120 * 80 * panelSetScale()
			clampPanelScroll()
			invalidate(h)
		}
		return 0
	case WM_RBUTTONUP:
		if !isPromotedFromTouch() {
			showMenu()
		}
		return 0

	case WM_MEASUREITEM:
		onMeasureItem(lparam)
		return 1
	case WM_DRAWITEM:
		onDrawItem(lparam)
		return 1
	case WM_APP_REFRESH:
		invalidate(h)
		return 0
	case WM_PAINT:
		onPaint()
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_SIZE:
		invalidate(h)
		return 0
	case WM_TIMER:
		if wparam == animTimer {
			onAnim()
		} else {
			onTimer()
		}
		return 0
	case WM_DISPLAYCHANGE, WM_DPICHANGED:
		placeWindow()
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, msg, wparam, lparam)
	return r
}

func pickFonts() {
	if !fontExists(iconFont) {
		iconFont = "Segoe MDL2 Assets"
	}
	for _, f := range []string{"Segoe UI Variable Display", "Segoe UI"} {
		if fontExists(f) {
			textFont = f
			break
		}
	}
	semiFont, semiStyle = textFont, FontStyleBold
	for _, f := range []string{"Segoe UI Variable Display Semibold", "Segoe UI Variable Display Semib", "Segoe UI Semibold"} {
		if fontExists(f) {
			semiFont, semiStyle = f, FontStyleRegular
			break
		}
	}
}

func main() {
	runtime.LockOSThread()

	if r, _, _ := pSetProcessDpiAwarenessContext.Call(^uintptr(3)); r == 0 {
		pSetProcessDPIAware.Call()
	}

	// Si la versión antigua (Panel Táctil) sigue abierta, avisar.
	if _, _, e := pCreateMutexW.Call(0, 0, wstrPtr("PanelTactil_UnicaInstancia")); e != nil {
		if errno, ok := e.(syscall.Errno); ok && errno == 183 {
			messageBox("Panel Táctil (la versión anterior) sigue abierto.\n\nCiérralo desde Engranaje → Salir y vuelve a abrir TapBoard.", "TapBoard")
			return
		}
	}
	// Después de actualizar, la versión anterior puede tardar un momento en cerrarse.
	for try := 0; ; try++ {
		h, _, err := pCreateMutexW.Call(0, 0, wstrPtr("TapBoard_UnicaInstancia"))
		if errno, ok := err.(syscall.Errno); ok && errno == 183 {
			pCloseHandle.Call(h)
			if justUpdated() && try < 40 {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return
		}
		instanceMutex = h
		break
	}

	pid, _, _ := pGetCurrentProcessId.Call()
	ownPid = uint32(pid)

	pCoInitializeEx.Call(0, 2)
	if !gdiplusInit() {
		messageBox("No se pudo iniciar GDI+.", "TapBoard")
		return
	}
	cfg = loadConfig()
	if versionNewer("2.3.0", cfg.SeenVersion) {
		cfg.Theme = "glass" // el rediseño de la 2.3.0 se estrena activado
		saveConfig(cfg)
	}
	if exe, err := os.Executable(); err == nil {
		migrateAutostart(exe)
		if autostartEnabled() {
			setAutostart(true, exe) // por si el .exe cambió de nombre o de carpeta
		}
	}
	pickFonts()
	loadIcons()

	demo = os.Getenv("PANEL_DEMO") != ""
	if demo {
		aud = AudioState{OK: true, Volume: 0.36, MicOK: true, DeviceName: "Altavoces (ATK N9 PRO dongle)"}
	} else {
		audio = newAudio()
		aud = audio.State()
	}

	hInst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	className := wstr("TapBoardVentana")
	wc := WNDCLASSEXW{
		Style:         3,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hInst,
		HCursor:       cursor,
		LpszClassName: className,
		HIcon:         appIcon16(hInst, 32),
		HIconSm:       appIcon16(hInst, 16),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	exStyle := uintptr(WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW)
	if cfg.SiempreEncima {
		exStyle |= WS_EX_TOPMOST
	}
	hwnd, _, _ = pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(className)),
		wstrPtr("TapBoard"), WS_POPUP, 0, 0, 800, 480, 0, 0, hInst, 0)
	if hwnd == 0 {
		messageBox("No se pudo crear la ventana.", "TapBoard")
		return
	}
	placeWindow()
	pShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
	resetWhatsApp()
	go whatsappWatcher()
	go updateWorker()
	go cleanupOldExe()
	if justUpdated() {
		toast("TapBoard actualizado a v" + appVersion)
	}
	maybeShowWhatsNew()
	go appsWorker()
	go pcMonitorWorker()
	if !demo {
		go mediaWorker()
	} else if os.Getenv("PANEL_DEMO_MEDIA") != "" {
		demoMedia()
	}
	if os.Getenv("PANEL_SETTINGS") != "" {
		openSettings()
	}
	pSetTimer.Call(hwnd, 1, 100, 0)

	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// moveWindowToNextMonitor mueve la ventana activa al siguiente monitor,
// saltándose el mini monitor del panel. Mantiene posición y tamaño
// relativos; si estaba maximizada o a pantalla completa, sigue así.
func moveWindowToNextMonitor(w uintptr) {
	if w == 0 || isOurWindow(w) {
		return
	}
	if ok, _, _ := pIsWindow.Call(w); ok == 0 {
		return
	}
	monitorsFound = nil
	pEnumDisplayMonitors.Call(0, 0, enumMonitorsCB, 0)
	var mons []monitor
	for _, m := range monitorsFound {
		if m.rect != panelRect {
			mons = append(mons, m)
		}
	}
	if len(mons) < 2 {
		flash(tDesktop, "Solo hay un monitor")
		return
	}
	var wr RECT
	pGetWindowRect.Call(w, uintptr(unsafe.Pointer(&wr)))
	cx, cy := (wr.L+wr.R)/2, (wr.T+wr.B)/2
	cur := 0
	for i, m := range mons {
		if cx >= m.rect.L && cx < m.rect.R && cy >= m.rect.T && cy < m.rect.B {
			cur = i
			break
		}
	}
	src, dst := mons[cur], mons[(cur+1)%len(mons)]

	if iconic, _, _ := pIsIconic.Call(w); iconic != 0 {
		pShowWindow.Call(w, 9) // SW_RESTORE
	}
	zoomed, _, _ := pIsZoomed.Call(w)
	fullscreen := wr == src.rect

	switch {
	case fullscreen:
		r := dst.rect
		pSetWindowPos.Call(w, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x0004|0x0010) // NOZORDER|NOACTIVATE
	case zoomed != 0:
		pShowWindow.Call(w, 9) // SW_RESTORE
		pGetWindowRect.Call(w, uintptr(unsafe.Pointer(&wr)))
		placeRelative(w, wr, src.work, dst.work)
		pShowWindow.Call(w, 3) // SW_MAXIMIZE
	default:
		placeRelative(w, wr, src.work, dst.work)
	}
	pSetForegroundWindow.Call(w)
}

func placeRelative(w uintptr, wr, src, dst RECT) {
	sw, sh := float64(src.R-src.L), float64(src.B-src.T)
	dw, dh := float64(dst.R-dst.L), float64(dst.B-dst.T)
	if sw <= 0 || sh <= 0 {
		return
	}
	ww, wh := float64(wr.R-wr.L), float64(wr.B-wr.T)
	if ww > dw {
		ww = dw
	}
	if wh > dh {
		wh = dh
	}
	x := float64(dst.L) + float64(wr.L-src.L)*dw/sw
	y := float64(dst.T) + float64(wr.T-src.T)*dh/sh
	if x+ww > float64(dst.R) {
		x = float64(dst.R) - ww
	}
	if y+wh > float64(dst.B) {
		y = float64(dst.B) - wh
	}
	if x < float64(dst.L) {
		x = float64(dst.L)
	}
	if y < float64(dst.T) {
		y = float64(dst.T)
	}
	pSetWindowPos.Call(w, 0, uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(ww)), uintptr(int32(wh)), 0x0004|0x0010)
}

func windowClass(w uintptr) string {
	buf := make([]uint16, 256)
	pGetClassNameW.Call(w, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}

func windowTitleShort(w uintptr) string {
	buf := make([]uint16, 256)
	pGetWindowTextW.Call(w, uintptr(unsafe.Pointer(&buf[0])), 256)
	t := syscall.UTF16ToString(buf)
	// "Página - Google Chrome" -> "Google Chrome"
	if i := strings.LastIndex(t, " - "); i >= 0 && i+3 < len(t) {
		t = t[i+3:]
	}
	r := []rune(t)
	if len(r) > 22 {
		t = string(r[:21]) + "…"
	}
	if t == "" {
		t = "la ventana"
	}
	return t
}

// closableWindow evita cerrar el escritorio o la barra de tareas.
func closableWindow(w uintptr) bool {
	if w == 0 || isOurWindow(w) {
		return false
	}
	if ok, _, _ := pIsWindow.Call(w); ok == 0 {
		return false
	}
	switch windowClass(w) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return false
	}
	return true
}

var dbgSum = map[string]time.Duration{}

func dbgAdd(k string, t time.Time) {
	if debugPaint {
		dbgSum[k] += time.Since(t)
	}
}
