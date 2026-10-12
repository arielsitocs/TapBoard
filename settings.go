//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Ventana de configuración visual.

var (
	shell32x               = syscall.NewLazyDLL("shell32.dll")
	dwmapi                 = syscall.NewLazyDLL("dwmapi.dll")
	pGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pSHBrowseForFolderW    = shell32x.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW  = shell32x.NewProc("SHGetPathFromIDListW")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	sColBG     = 0xFF161616
	sColCard   = 0xFF202124
	sColLine   = 0xFF2C2D31
	sColText   = 0xFFF2F2F2
	sColSub    = 0xFF9AA0A8
	sColOff    = 0xFF44474D
	sColAccent = 0xFF2196F3
	sColDanger = 0xFF8C2D3A

	WM_MOUSEWHEEL = 0x020A
	WM_CLOSE      = 0x0010
)

type sHit struct {
	r  RectF
	fn func()
}

var (
	setHwnd    uintptr
	setScroll  float32
	setContent float32
	setHits    []sHit
	setPressed = -1
	setDevs    []AudioDevice
	setClassOK bool
)

func setScale() float32 {
	if setHwnd == 0 {
		return 1
	}
	d, _, _ := pGetDpiForWindow.Call(setHwnd)
	if d == 0 {
		return 1
	}
	return float32(d) / 96
}

func openSettingsWindow() {
	if setHwnd != 0 {
		pShowWindow.Call(setHwnd, 9) // SW_RESTORE
		pSetForegroundWindow.Call(setHwnd)
		return
	}
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := wstr("TapBoardConfig")
	if !setClassOK {
		cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
		wc := WNDCLASSEXW{
			Style:         3,
			LpfnWndProc:   syscall.NewCallback(settingsProc),
			HInstance:     hInst,
			HCursor:       cursor,
			LpszClassName: className,
			HIcon:         appIcon16(hInst, 32),
			HIconSm:       appIcon16(hInst, 16),
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		setClassOK = true
	}
	const style = 0x00C00000 | 0x00080000 | 0x00020000 // CAPTION | SYSMENU | MINIMIZEBOX
	setHwnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)),
		wstrPtr("Configuración · TapBoard"), style, 0, 0, 600, 700, 0, 0, hInst, 0)
	if setHwnd == 0 {
		return
	}
	dark := int32(1)
	pDwmSetWindowAttribute.Call(setHwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	if !demo {
		setDevs = audio.RenderDevices()
	}
	setScroll = 0
	placeSettings()
	pShowWindow.Call(setHwnd, SW_SHOWNORMAL)
	pSetForegroundWindow.Call(setHwnd)
}

// placeSettings centra la ventana en el monitor más grande (el principal).
func placeSettings() {
	monitorsFound = nil
	pEnumDisplayMonitors.Call(0, 0, enumMonitorsCB, 0)
	work := RECT{0, 0, 1280, 800}
	var best int64
	for _, m := range monitorsFound {
		if m.area > best {
			best, work = m.area, m.work
		}
	}
	// Mover primero al monitor para conocer su DPI.
	pSetWindowPos.Call(setHwnd, 0, uintptr(work.L+40), uintptr(work.T+40), 600, 700, 0x0004)
	s := setScale()
	cw := int32(560 * s)
	ch := int32(math.Min(float64(setContentHeight(s)), float64(work.B-work.T)*0.88))
	var wr, cr RECT
	pGetWindowRect.Call(setHwnd, uintptr(unsafe.Pointer(&wr)))
	pGetClientRect.Call(setHwnd, uintptr(unsafe.Pointer(&cr)))
	ww := cw + (wr.R - wr.L) - (cr.R - cr.L)
	wh := ch + (wr.B - wr.T) - (cr.B - cr.T)
	x := work.L + (work.R-work.L-ww)/2
	y := work.T + (work.B-work.T-wh)/2
	pSetWindowPos.Call(setHwnd, 0, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0x0004)
}

func setContentHeight(s float32) float32 {
	// Medida "en seco": se dibuja sin canvas.
	setHits = nil
	return layoutSettings(nil, 560*s, s)
}

func settingsProc(h, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		paintSettings(h)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_LBUTTONDOWN:
		setPressed = settingsHit(xParam(lparam), yParam(lparam))
		invalidate(h)
		return 0
	case WM_LBUTTONUP:
		i := settingsHit(xParam(lparam), yParam(lparam))
		p := setPressed
		setPressed = -1
		if i >= 0 && i == p {
			setHits[i].fn()
		}
		invalidate(h)
		return 0
	case WM_MOUSEWHEEL:
		d := float32(int16(wparam>>16)) / 120
		setScroll -= d * 70 * setScale()
		clampScroll()
		invalidate(h)
		return 0
	case WM_DPICHANGED:
		r := (*RECT)(unsafe.Pointer(lparam))
		pSetWindowPos.Call(h, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x0004|0x0010)
		invalidate(h)
		return 0
	case WM_SIZE:
		clampScroll()
		invalidate(h)
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(h)
		return 0
	case WM_DESTROY:
		setHwnd = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, msg, wparam, lparam)
	return r
}

func clampScroll() {
	var rc RECT
	pGetClientRect.Call(setHwnd, uintptr(unsafe.Pointer(&rc)))
	maxS := setContent - float32(rc.B-rc.T)
	if maxS < 0 {
		maxS = 0
	}
	if setScroll > maxS {
		setScroll = maxS
	}
	if setScroll < 0 {
		setScroll = 0
	}
}

func settingsHit(x, y int32) int {
	fx, fy := float32(x), float32(y)+setScroll
	for i := len(setHits) - 1; i >= 0; i-- {
		if inRect(setHits[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

func paintSettings(h uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&rc)))
	w, hh := rc.R-rc.L, rc.B-rc.T
	if w > 0 && hh > 0 {
		mem, _, _ := pCreateCompatibleDC.Call(hdc)
		bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(hh))
		old, _, _ := pSelectObject.Call(mem, bmp)
		c := newCanvas(mem)
		c.Clear(sColBG)
		c.Translate(0, -setScroll)
		setHits = nil
		setContent = layoutSettings(c, float32(w), setScale())
		c.ResetTransform()
		// barra de desplazamiento fina
		if setContent > float32(hh) {
			vh := float32(hh)
			bh := vh * vh / setContent
			by := setScroll / (setContent - vh) * (vh - bh)
			c.RoundRect(float32(w)-6*setScale(), by+2, 3*setScale(), bh-4, 2*setScale(), 0x50FFFFFF)
		}
		c.Close()
		pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(hh), mem, 0, 0, SRCCOPY)
		pSelectObject.Call(mem, old)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mem)
	}
	pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
}

// ------------------------------------------------------------ construcción

type sLayout struct {
	c        *Canvas // nil = solo medir
	s        float32
	w        float32
	y        float32
	cardTop  float32
	rowCount int
}

func (L *sLayout) hit(r RectF, fn func()) bool {
	setHits = append(setHits, sHit{r, fn})
	return setPressed == len(setHits)-1
}

func (L *sLayout) text(s string, font string, size float32, style int, col uint32, r RectF, ha int) {
	if L.c != nil {
		L.c.Text(s, font, size, style, col, r, ha, AlignCenter)
	}
}

var sectionPos = map[string]float32{}

func (L *sLayout) section(title string) {
	s := L.s
	sectionPos[title] = L.y
	L.y += 18 * s
	L.cardTop = L.y
	L.rowCount = 0
}

func (L *sLayout) endSection(draw func()) {
	_ = draw
}

// card dibuja el fondo de una tarjeta; se llama al cerrar la sección.
func (L *sLayout) cardBG(top, bottom float32) {
	if L.c == nil {
		return
	}
	s := L.s
	L.c.RoundRect(16*s, top, L.w-32*s, bottom-top, 12*s, sColCard)
}

type sRow struct {
	label, sub string
	h          float32
	control    func(L *sLayout, area RectF)
}

func (L *sLayout) card(rows []sRow) {
	s := L.s
	top := L.y
	total := float32(0)
	for _, r := range rows {
		total += r.h * s
	}
	L.cardBG(top, top+total)
	y := top
	for i, r := range rows {
		rh := r.h * s
		if i > 0 && L.c != nil {
			L.c.Line(32*s, y, L.w-32*s, y, 1, sColLine)
		}
		lx := 32 * s
		lw := L.w*0.55 - lx
		if r.sub != "" {
			L.text(r.label, textFont, 14.5*s, FontStyleRegular, sColText, RectF{lx, y + rh*0.18, lw, rh * 0.36}, AlignNear)
			L.text(r.sub, textFont, 12*s, FontStyleRegular, sColSub, RectF{lx, y + rh*0.52, lw + 60*s, rh * 0.32}, AlignNear)
		} else {
			L.text(r.label, textFont, 14.5*s, FontStyleRegular, sColText, RectF{lx, y, lw, rh}, AlignNear)
		}
		if r.control != nil {
			r.control(L, RectF{L.w * 0.5, y, L.w*0.5 - 32*s, rh})
		}
		y += rh
	}
	L.y = y
}

// --- controles

func toggleCtl(get func() bool, set func(bool), enabled bool) func(L *sLayout, a RectF) {
	return func(L *sLayout, a RectF) {
		s := L.s
		tw, th := 44*s, 24*s
		r := RectF{a.X + a.W - tw, a.Y + (a.H-th)/2, tw, th}
		on := get()
		if enabled {
			L.hit(RectF{r.X - 10*s, a.Y, tw + 20*s, a.H}, func() { set(!on); afterSettingChange() })
		}
		if L.c == nil {
			return
		}
		track := uint32(sColOff)
		if on {
			track = sColAccent
		}
		knob := uint32(0xFFFFFFFF)
		if !enabled {
			track, knob = 0xFF2E3034, 0xFF6A6D72
		}
		L.c.RoundRect(r.X, r.Y, r.W, r.H, th/2, track)
		kr := th/2 - 3*s
		kx := r.X + th/2
		if on {
			kx = r.X + r.W - th/2
		}
		L.c.Circle(kx, r.Y+th/2, kr, knob)
	}
}

func segmentCtl(opts []string, get func() int, set func(int)) func(L *sLayout, a RectF) {
	return func(L *sLayout, a RectF) {
		s := L.s
		h := 32 * s
		longest := 0
		for _, o := range opts {
			if n := len([]rune(o)); n > longest {
				longest = n
			}
		}
		segW := float32(longest)*8.2*s + 30*s
		if segW < 52*s {
			segW = 52 * s
		}
		total := segW * float32(len(opts))
		if total > a.W {
			segW = a.W / float32(len(opts))
			total = a.W
		}
		x0 := a.X + a.W - total
		y := a.Y + (a.H-h)/2
		if L.c != nil {
			L.c.RoundRect(x0, y, total, h, 8*s, 0xFF2B2D31)
		}
		cur := get()
		for i, o := range opts {
			r := RectF{x0 + segW*float32(i), y, segW, h}
			i := i
			pr := L.hit(r, func() { set(i); afterSettingChange() })
			if L.c == nil {
				continue
			}
			col := uint32(sColSub)
			if i == cur {
				L.c.RoundRect(r.X+3*s, r.Y+3*s, r.W-6*s, r.H-6*s, 6*s, sColAccent)
				col = 0xFFFFFFFF
			} else if pr {
				L.c.RoundRect(r.X+3*s, r.Y+3*s, r.W-6*s, r.H-6*s, 6*s, 0xFF383B40)
			}
			L.c.Text(o, semiFont, 12.5*s, semiStyle, col, r, AlignCenter, AlignCenter)
		}
	}
}

func buttonCtl(labels []string, cols []uint32, fns []func()) func(L *sLayout, a RectF) {
	return func(L *sLayout, a RectF) {
		s := L.s
		h := 32 * s
		x := a.X + a.W
		for i := len(labels) - 1; i >= 0; i-- {
			bw := float32(len([]rune(labels[i])))*7.6*s + 28*s
			x -= bw
			r := RectF{x, a.Y + (a.H-h)/2, bw, h}
			pr := L.hit(r, fns[i])
			if L.c != nil {
				col := cols[i]
				if pr {
					col = lighten(col, 0.15)
				}
				L.c.RoundRect(r.X, r.Y, r.W, r.H, 8*s, col)
				L.c.Text(labels[i], semiFont, 12.5*s, semiStyle, 0xFFFFFFFF, r, AlignCenter, AlignCenter)
			}
			x -= 8 * s
		}
	}
}

func checkRow(L *sLayout, label string, on bool, fn func()) {
	s := L.s
	rh := 44 * s
	r := RectF{16 * s, L.y, L.w - 32*s, rh}
	L.hit(r, func() { fn(); afterSettingChange() })
	if L.c != nil {
		bx, by, bs := 32*s, L.y+(rh-20*s)/2, 20*s
		if on {
			L.c.RoundRect(bx, by, bs, bs, 5*s, sColAccent)
			L.c.Line(bx+bs*0.25, by+bs*0.52, bx+bs*0.43, by+bs*0.7, 2.2*s, 0xFFFFFFFF)
			L.c.Line(bx+bs*0.43, by+bs*0.7, bx+bs*0.76, by+bs*0.32, 2.2*s, 0xFFFFFFFF)
		} else {
			L.c.RoundRect(bx, by, bs, bs, 5*s, 0xFF5A5E66)
			L.c.RoundRect(bx+1.5*s, by+1.5*s, bs-3*s, bs-3*s, 4*s, sColCard)
		}
		L.c.Text(label, textFont, 14*s, FontStyleRegular, sColText,
			RectF{bx + bs + 12*s, L.y, L.w - bx - bs - 60*s, rh}, AlignNear, AlignCenter)
	}
	L.y += rh
}

// --- contenido

func layoutSettings(c *Canvas, w, s float32) float32 {
	L := &sLayout{c: c, s: s, w: w}
	L.y = 0
	if settingsTab == 0 {
		// Pantalla
		monitorsFound = nil
		pEnumDisplayMonitors.Call(0, 0, enumMonitorsCB, 0)
		mons := []string{"Auto"}
		sub := "Auto usa el monitor más pequeño"
		for i, m := range monitorsFound {
			mons = append(mons, fmt.Sprint(i+1))
			if i < 3 {
				sub += fmt.Sprintf(" · %d: %d×%d", i+1, m.rect.R-m.rect.L, m.rect.B-m.rect.T)
			}
		}
		if len(mons) > 5 {
			mons = mons[:5]
		}
		L.section("PANTALLA")
		L.card([]sRow{
			{"Monitor del panel", "", 64, segmentCtl(mons,
				func() int { return cfg.Monitor },
				func(i int) { cfg.Monitor = i })},
			{"Tema", "", 64, segmentCtl(themeLabels,
				themeIndex,
				func(i int) { cfg.Theme = themeKeys[i] })},
			{"Reposo", "", 64, segmentCtl([]string{"Nunca", "1", "3", "5", "10"},
				idleIndex,
				func(i int) { cfg.IdleMinutes = idleOptions[i] })},
			{"Siempre encima", "", 64, toggleCtl(
				func() bool { return cfg.SiempreEncima },
				func(b bool) { cfg.SiempreEncima = b }, true)},
			{"Sonido al tocar", "", 64, toggleCtl(
				func() bool { return cfg.Sonidos },
				func(b bool) { cfg.Sonidos = b }, true)},
			{"Iniciar con Windows", "", 52, toggleCtl(
				autostartEnabled,
				func(b bool) { exe, _ := os.Executable(); setAutostart(b, exe) }, true)},
		})

	}
	if settingsTab == 1 {
		// Botones
		folder := cfg.Carpeta
		if folder == "" {
			folder = "Descargas"
		} else if r := []rune(folder); len(r) > 42 {
			folder = "…" + string(r[len(r)-41:])
		}
		moon := 0
		if strings.EqualFold(cfg.AccionLuna, "screenOff") {
			moon = 1
		}
		_ = moon
		L.section("BOTONES")
		L.card([]sRow{
			{"Carpeta del botón de descargas", folder, 64, buttonCtl(
				[]string{"Descargas", "Cambiar…"},
				[]uint32{0xFF34373C, sColAccent},
				[]func(){
					func() { cfg.Carpeta = ""; afterSettingChange() },
					func() {
						if p := pickFolder(); p != "" {
							cfg.Carpeta = p
							afterSettingChange()
						}
					},
				})},
			{"Botón de la luna", "", 64, segmentCtl([]string{"Suspender", "Apagar pantalla"},
				func() int {
					if strings.EqualFold(cfg.AccionLuna, "screenOff") {
						return 1
					}
					return 0
				},
				func(i int) {
					if i == 1 {
						cfg.AccionLuna = "screenOff"
					} else {
						cfg.AccionLuna = "sleep"
					}
				})},
			{"Confirmar luna", "", 64, toggleCtl(
				func() bool { return cfg.ConfirmarLuna },
				func(b bool) { cfg.ConfirmarLuna = b }, true)},
			{"Confirmar apagado", "", 64, toggleCtl(
				func() bool { return cfg.ConfirmarApagar },
				func(b bool) { cfg.ConfirmarApagar = b }, true)},
		})

	}
	if settingsTab == 2 {
		// Página 2
		L.section("PÁGINAS DE BOTONES")
		slotGrid(L, -1, 5, 5, "Página 1 · fila de abajo", false)
		for bp := 0; bp < numButtonPages(); bp++ {
			slotGrid(L, bp, 8, 4, "Página "+itoa(panelPageOfBP(bp)+1), bp > 0)
		}
		{
			L.y += 12 * s
			bh := 44 * s
			br := RectF{16 * s, L.y, w - 32*s, bh}
			pr := L.hit(br, func() { addButtonPage(); afterSettingChange() })
			if c != nil {
				col := uint32(0xFF26282C)
				if pr {
					col = lighten(col, 0.12)
				}
				c.RoundRect(br.X, br.Y, br.W, br.H, 12*s, col)
				c.Text("+  Agregar página de botones", semiFont, 14*s, semiStyle, sColAccent, br, AlignCenter, AlignCenter)
			}
			L.y += bh
		}

	}
	if settingsTab == 3 {
		// Audio
		L.section("BOTÓN DE AUDÍFONOS")
		top := L.y
		n := len(setDevs)
		if n == 0 {
			n = 1
		}
		L.cardBG(top, top+float32(n)*44*s)
		if len(setDevs) == 0 {
			L.text("No se encontraron salidas de audio.", textFont, 14*s, FontStyleRegular, sColSub,
				RectF{32 * s, L.y, w - 64*s, 44 * s}, AlignNear)
			L.y += 44 * s
		}
		for _, d := range setDevs {
			d := d
			checkRow(L, d.Name, deviceSelected(d), func() { toggleDevice(d) })
		}

	}
	if settingsTab == 4 {
		// Spotify y WhatsApp
		L.section("MÚSICA Y WHATSAPP")
		L.card([]sRow{
			{"Lo que suena", "", 64, toggleCtl(
				func() bool { return cfg.MostrarSpotify },
				func(b bool) { cfg.MostrarSpotify = b }, true)},
			{"Notificaciones de WhatsApp", "", 64, toggleCtl(
				func() bool { return cfg.WhatsApp },
				func(b bool) { cfg.WhatsApp = b; resetWhatsApp() }, true)},
			{"Mostrar el mensaje", "", 64, toggleCtl(
				func() bool { return cfg.WhatsAppTexto },
				func(b bool) { cfg.WhatsAppTexto = b }, cfg.WhatsApp)},
		})

	}
	if settingsTab == 5 {
		// Respaldo
		L.section("RESPALDO")
		L.card([]sRow{
			{"Copia de seguridad", "", 64, buttonCtl(
				[]string{"Importar…", "Exportar…"}, []uint32{0xFF34373C, sColAccent},
				[]func(){importConfig, exportConfig})},
		})

		// Otros
		L.section("OTROS")
		L.card([]sRow{
			{"Archivo de configuración", "", 64, buttonCtl(
				[]string{"Abrir config.json"}, []uint32{0xFF34373C},
				[]func(){func() {
					pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("notepad.exe"), wstrPtr(`"`+configPath()+`"`), 0, SW_SHOWNORMAL)
				}})},
			{"Recargar", "", 64, buttonCtl(
				[]string{"Recargar"}, []uint32{0xFF34373C},
				[]func(){func() { cfg = loadConfig(); placeWindow(); invalidate(setHwnd) }})},
			{"Cerrar el panel", "", 64, buttonCtl(
				[]string{"Salir"}, []uint32{sColDanger},
				[]func(){func() { pPostQuitMessage.Call(0) }})},
		})
	}
	L.y += 24 * s
	return L.y
}

// ---- cabecera fija de la configuración (título, volver y pestañas)

var (
	settingsTab = 0
	headHits    []sHit
	headPressed = -1
)

var settingsTabs = []string{"Pantalla", "Acciones", "Botones", "Audio", "Música", "Sistema"}

func settingsHeaderH(s float32) float32 { return 22*s + 46*s + 36*s + 12*s }

func paintSettingsHeader(c *Canvas, w, s float32) {
	headHits = nil
	hit := func(r RectF, fn func()) bool {
		headHits = append(headHits, sHit{r, fn})
		return headPressed == len(headHits)-1
	}
	hh := settingsHeaderH(s)
	c.RoundRect(-2, -2, w+4, hh+2, 0, themeBG())
	c.Line(16*s, hh-1, w-16*s, hh-1, 1, 0xFF2C2D31)
	y := 22 * s
	c.Text("Configuración", semiFont, 24*s, semiStyle, sColText, RectF{24 * s, y, w * 0.6, 34 * s}, AlignNear, AlignCenter)
	{
		bw, bh := 104*s, 36*s
		br := RectF{w - 24*s - bw, y - 1*s, bw, bh}
		col := uint32(0xFF34373C)
		if hit(br, closeSettings) {
			col = lighten(col, 0.15)
		}
		c.RoundRect(br.X, br.Y, br.W, br.H, bh/2, col)
		c.Text("Volver", semiFont, 14*s, semiStyle, sColText, br, AlignCenter, AlignCenter)
	}
	if setMsg != "" && time.Now().Before(setMsgUntil) {
		// aviso temporal (por ejemplo "Respaldo guardado") junto al título
		c.Text(setMsg, semiFont, 13*s, semiStyle, 0xFF81C784, RectF{w * 0.38, y, w*0.62 - 140*s, 34 * s}, AlignFar, AlignCenter)
	}
	y += 46 * s
	// pestañas (cada una es una página)
	n := len(settingsTabs)
	gap := 8 * s
	tw := (w - 32*s - gap*float32(n-1)) / float32(n)
	th := 36 * s
	for i, t := range settingsTabs {
		i := i
		r := RectF{16*s + float32(i)*(tw+gap), y, tw, th}
		pr := hit(r, func() {
			if settingsTab != i {
				settingsTab = i
				setScroll = 0
			}
			invalidate(hwnd)
		})
		col, tc := uint32(0xFF2A2C31), uint32(sColText)
		if i == settingsTab {
			col, tc = sColAccent, 0xFFFFFFFF
		} else if pr {
			col = lighten(col, 0.15)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, th/2, col)
		c.Text(t, semiFont, 13.5*s, semiStyle, tc, r, AlignCenter, AlignCenter)
	}
}

func afterSettingChange() {
	saveConfig(cfg)
	placeWindow()
	invalidate(hwnd)
	if setHwnd != 0 {
		invalidate(setHwnd)
	}
}

func normName(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func deviceSelected(d AudioDevice) bool {
	n := normName(d.Name)
	for _, f := range cfg.DispositivosAudio {
		if f := normName(f); f != "" && strings.Contains(n, f) {
			return true
		}
	}
	return false
}

func toggleDevice(d AudioDevice) {
	if deviceSelected(d) {
		n := normName(d.Name)
		var keep []string
		for _, f := range cfg.DispositivosAudio {
			if nf := normName(f); nf != "" && strings.Contains(n, nf) {
				continue
			}
			keep = append(keep, f)
		}
		if keep == nil {
			keep = []string{}
		}
		cfg.DispositivosAudio = keep
	} else {
		cfg.DispositivosAudio = append(cfg.DispositivosAudio, ShortName(d.Name))
	}
}

func pickFolder() string {
	type browseInfo struct {
		Owner       uintptr
		Root        uintptr
		DisplayName *uint16
		Title       *uint16
		Flags       uint32
		Fn          uintptr
		LParam      uintptr
		Image       int32
	}
	name := make([]uint16, 260)
	bi := browseInfo{Owner: hwnd, DisplayName: &name[0], Title: wstr("Elige la carpeta para el botón de descargas"), Flags: 0x0001 | 0x0040}
	pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pidl)
	path := make([]uint16, 1024)
	if r, _, _ := pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

// ------------------------------------------------------------ dentro del panel

var (
	settingsOpen  bool
	settingsAnim  float32
	setDownY      int32
	setDownScroll float32
	setScrolling  bool
)

func openSettings() {
	if !demo {
		setDevs = audio.RenderDevices()
	}
	setScroll = 0
	setPressed = -1
	settingsOpen = true
	pendingTile = -1
	startAnim()
}

func closeSettings() {
	settingsOpen = false
	setPressed = -1
	startAnim()
}

func panelSetScale() float32 { return panelH / 480 }

func panelSetGeom() (ox, cw, oy float32) {
	s := panelSetScale()
	cw = min32(panelW, 820*s)
	ox = (panelW - cw) / 2
	e := 1 - (1-settingsAnim)*(1-settingsAnim)*(1-settingsAnim)
	oy = (1 - e) * panelH
	return
}

func paintSettingsPanel(c *Canvas) {
	if settingsAnim < 0.01 || pickerAnim >= 0.999 {
		return
	}
	ox, cw, oy := panelSetGeom()
	c.ResetTransform()
	c.RoundRect(0, oy, panelW, panelH+pad*2, pad*2*(1-settingsAnim), themeBG())
	sc := panelSetScale()
	hh := settingsHeaderH(sc)
	c.Translate(ox, oy+hh-setScroll)
	setHits = nil
	setContent = layoutSettings(c, cw, sc)
	// cabecera fija encima del contenido
	c.Translate(ox, oy)
	paintSettingsHeader(c, cw, sc)
	c.ResetTransform()
	if vis := panelH - hh; setContent > vis {
		bh := vis * vis / setContent
		by := setScroll / (setContent - vis) * (vis - bh)
		c.RoundRect(panelW-6*sc, oy+hh+by+3, 3*sc, bh-6, 2*sc, 0x50FFFFFF)
	}
}

func clampPanelScroll() {
	maxS := setContent - (panelH - settingsHeaderH(panelSetScale()))
	if maxS < 0 {
		maxS = 0
	}
	if setScroll > maxS {
		setScroll = maxS
	}
	if setScroll < 0 {
		setScroll = 0
	}
}

func panelSetHit(x, y int32) int {
	ox, _, oy := panelSetGeom()
	hh := settingsHeaderH(panelSetScale())
	if float32(y)-oy < hh {
		return -1 // es la cabecera
	}
	fx, fy := float32(x)-ox, float32(y)-oy-hh+setScroll
	for i := len(setHits) - 1; i >= 0; i-- {
		if inRect(setHits[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

// floatingBack es un botón "Volver" fijo que aparece al bajar en la lista.
func floatingBack() (RectF, bool) {
	s := panelSetScale()
	if true || setScroll < 60*s || settingsAnim < 0.9 { // ya no se usa: la cabecera es fija
		return RectF{}, false
	}
	ox, cw, oy := panelSetGeom()
	bw, bh := 104*s, 36*s
	return RectF{ox + cw - 24*s - bw, oy + 14*s, bw, bh}, true
}

func settingsDown(x, y int32) {
	setDownY = y
	setDownScroll = setScroll
	setScrolling = false
	if fr, ok := floatingBack(); ok && inRect(fr, float32(x), float32(y)) {
		clickSound()
		setPressed = -2
		invalidate(hwnd)
		return
	}
	if h := headHit(x, y); h >= 0 {
		headPressed = h
		setPressed = -3
		clickSound()
		invalidate(hwnd)
		return
	}
	setPressed = panelSetHit(x, y)
	if setPressed >= 0 {
		clickSound()
	}
	invalidate(hwnd)
}

func headHit(x, y int32) int {
	ox, _, oy := panelSetGeom()
	fx, fy := float32(x)-ox, float32(y)-oy
	for i := len(headHits) - 1; i >= 0; i-- {
		if inRect(headHits[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

func settingsMove(x, y int32) {
	dy := float32(y - setDownY)
	if !setScrolling && math.Abs(float64(dy)) > float64(panelH*0.025) {
		setScrolling = true
		setPressed = -1
	}
	if setScrolling {
		setScroll = setDownScroll - dy
		clampPanelScroll()
		invalidate(hwnd)
	}
}

func settingsUp(x, y int32) {
	if setPressed == -3 {
		setPressed = -1
		h := headPressed
		headPressed = -1
		if headHit(x, y) == h && h >= 0 && h < len(headHits) {
			headHits[h].fn()
		}
		setScrolling = false
		invalidate(hwnd)
		return
	}
	if setPressed == -2 {
		setPressed = -1
		if fr, ok := floatingBack(); ok && inRect(fr, float32(x), float32(y)) {
			closeSettings()
		}
		invalidate(hwnd)
		return
	}
	i := panelSetHit(x, y)
	p := setPressed
	setPressed = -1
	if !setScrolling && i >= 0 && i == p && i < len(setHits) {
		setHits[i].fn()
	}
	setScrolling = false
	invalidate(hwnd)
}

// slotGrid dibuja una miniatura editable de una página de botones.
func slotGrid(L *sLayout, bp, n, cols int, title string, removable bool) {
	s, w, c := L.s, L.w, L.c
	L.y += 6 * s
	L.text(title, semiFont, 14*s, semiStyle, sColText, RectF{24 * s, L.y, w * 0.6, 28 * s}, AlignNear)
	if removable {
		bw, bh := 132*s, 28*s
		br := RectF{w - 16*s - bw, L.y, bw, bh}
		pr := L.hit(br, func() { removeButtonPage(bp); afterSettingChange() })
		if c != nil {
			col := uint32(0xFF4A2C30)
			if pr {
				col = lighten(col, 0.15)
			}
			c.RoundRect(br.X, br.Y, br.W, br.H, bh/2, col)
			c.Text("Quitar página", semiFont, 12*s, semiStyle, 0xFFFFB4B4, br, AlignCenter, AlignCenter)
		}
	}
	L.y += 34 * s
	gx, gw := 16*s, w-32*s
	rows := (n + cols - 1) / cols
	gp := 10 * s
	cw := (gw - gp*float32(cols+1)) / float32(cols)
	ch := 92 * s
	L.cardBG(L.y, L.y+gp+float32(rows)*(ch+gp))
	for i := 0; i < n; i++ {
		i := i
		x := gx + gp + float32(i%cols)*(cw+gp)
		y := L.y + gp + float32(i/cols)*(ch+gp)
		r := RectF{x, y, cw, ch}
		pr := L.hit(r, func() { openPicker(bp, i) })
		if c != nil {
			img, label, col := slotVisual(bp, i)
			col = themeTile(col)
			if pr {
				col = lighten(col, 0.15)
			}
			c.RoundRectGrad(r.X, r.Y, r.W, r.H, 12*s, lighten(col, 0.07), darken(col, 0.06))
			is := 40 * s
			if img != nil {
				c.DrawImage(img, r.X+(r.W-is)/2, r.Y+12*s, is, is)
			}
			c.Text(label, semiFont, 12*s, semiStyle, 0xEEFFFFFF, RectF{r.X + 6*s, r.Y + 58*s, r.W - 12*s, 26 * s}, AlignCenter, AlignCenter)
		}
	}
	L.y += gp + float32(rows)*(ch+gp)
}
