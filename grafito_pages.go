//go:build windows

package main

// Tema Grafito: monitor del PC y encabezados de página.

import (
	"fmt"
	"math"
	"time"
	"unsafe"
)

const gWarn = 0xFFFFAA4D

// gHeader: título de página arriba a la izquierda y un dato tenue a la derecha.
// Devuelve dónde empieza el contenido.
func gHeader(c *Canvas, title, right string) float32 {
	fs := panelH * 0.037
	y := pad * 1.4
	h := fs * 1.6
	c.Text(title, semiFont, fs, semiStyle, gText, RectF{pad * 1.6, y, panelW * 0.6, h}, AlignNear, AlignCenter)
	c.Text(right, textFont, fs*0.64, FontStyleRegular, gMuted, RectF{panelW * 0.4, y, panelW*0.6 - pad*1.6, h}, AlignFar, AlignCenter)
	return y + h + pad*0.9
}

func gMonRect(i int, top float32) RectF {
	cols, rows := 3, 2
	bottom := pad*1.2 + 10
	cw := (panelW - pad*float32(cols+1)) / float32(cols)
	ch := (panelH - top - pad*float32(rows-1) - bottom) / float32(rows)
	return RectF{pad + float32(i%cols)*(cw+pad), top + float32(i/cols)*(ch+pad), cw, ch}
}

type gStat struct {
	name, right string
	rightCol    uint32
	value, unit string
	valueCol    uint32
	line        string
	hist        []float64
	histMax     float64
	barFrac     float64 // < 0 = sin barra horizontal
	col         uint32  // color propio de la métrica
	gauge       float64 // 0-100 = medidor circular junto al valor; < 0 = sin medidor
}

// Colores de cada métrica: tonos claros y vivos, distintos entre sí pero con
// la misma luminosidad, para que el conjunto se vea parejo.
const (
	gColCPU  = 0xFF8FA6FF // azul lavanda
	gColGPU  = 0xFF5FE3A1 // menta
	gColRAM  = 0xFFC39BFF // violeta
	gColDisk = 0xFFFFC266 // ámbar
	gColNet  = 0xFF5CCFF0 // celeste
	gColSys  = 0xFFB9BFCC // gris claro
)

func gStatCard(c *Canvas, r RectF, st gStat) {
	col := st.col
	if col == 0 {
		col = gAccent
	}
	if st.valueCol == gWarn {
		col = gWarn // carga alta: toda la tarjeta avisa en naranja
	}
	// fondo con un tinte muy leve del color de la métrica arriba
	c.RoundRectGrad(r.X, r.Y, r.W, r.H, gRadius(r), mix(gSurface, col, 0.10), gSurface)
	p := r.H * 0.1
	ns := r.H * 0.062
	// encabezado: punto de color + NOMBRE · dato a la derecha
	c.Circle(r.X+p+ns*0.32, r.Y+p+ns*0.75, ns*0.32, col)
	c.Text(upper(st.name), semiFont, ns, semiStyle, mix(col, gText, 0.35), RectF{r.X + p + ns*0.95, r.Y + p, r.W * 0.5, ns * 1.5}, AlignNear, AlignCenter)
	rc := st.rightCol
	if rc == 0 {
		rc = gMuted
	}
	c.Text(st.right, textFont, ns*1.05, FontStyleRegular, rc, RectF{r.X + r.W*0.35, r.Y + p, r.W*0.65 - p, ns * 1.5}, AlignFar, AlignCenter)
	// valor grande + unidad
	vs := r.H * 0.2
	vy := r.Y + p + ns*1.9
	vc := st.valueCol
	if vc == 0 {
		vc = gText
	}
	vw := textWidth(c, st.value, semiFont, vs, semiStyle)
	c.Text(st.value, semiFont, vs, semiStyle, vc, RectF{r.X + p, vy, r.W - 2*p, vs * 1.3}, AlignNear, AlignCenter)
	if st.unit != "" {
		us := vs * 0.4
		c.Text(st.unit, semiFont, us, semiStyle, gMuted, RectF{r.X + p + vw + vs*0.18, vy + vs*0.36, r.W - 2*p - vw, vs * 0.85}, AlignNear, AlignCenter)
	}
	lineW := r.W - 2*p
	// medidor circular a la derecha del valor
	if st.gauge >= 0 {
		gr := r.H * 0.125
		cx, cy := r.X+r.W-p-gr, vy+vs*0.68
		w := gr * 0.26
		c.Arc(cx, cy, gr, 0, 360, w, gTrack)
		if f := math.Min(st.gauge, 100) / 100; f > 0.005 {
			c.Arc(cx, cy, gr, -90, float32(360*f), w, col)
		}
		lineW -= gr*2 + p*0.5
	}
	ly := vy + vs*1.3
	if st.line != "" {
		ls := r.H * 0.058
		// hasta dos líneas: junto al medidor el texto largo (GPU · VRAM) no se corta
		c.TextWrap(st.line, textFont, ls, FontStyleRegular, gMuted, RectF{r.X + p, ly, lineW, ls * 2.7}, AlignNear, AlignNear)
	}
	// abajo: gráfico del último minuto, o barra de uso (disco)
	if st.barFrac >= 0 {
		th := r.H * 0.055
		y := r.Y + r.H - p - th
		c.RoundRect(r.X+p, y, r.W-2*p, th, th/2, gTrack)
		if st.barFrac > 0 {
			c.RoundRect(r.X+p, y, (r.W-2*p)*float32(math.Min(st.barFrac, 1)), th, th/2, col)
		}
		ps := r.H * 0.052
		c.Text(fmt.Sprintf("%.0f %% usado", st.barFrac*100), textFont, ps, FontStyleRegular, gMuted,
			RectF{r.X + p, y - ps*1.8, r.W - 2*p, ps * 1.5}, AlignFar, AlignCenter)
		return
	}
	gAreaChart(c, r, st.hist, st.histMax, col)
}

// gAreaChart: gráfico de área del último minuto al fondo de la tarjeta, de
// borde a borde. Línea suave con relleno tenue, una guía al 50 % y un punto
// en el valor actual. Con pocos datos crece desde la derecha.
func gAreaChart(c *Canvas, r RectF, hist []float64, maxV float64, col uint32) {
	if len(hist) < 2 {
		return
	}
	h := hist
	if len(h) > histLen {
		h = h[len(h)-histLen:]
	}
	// suavizado ligero (media de 3) para que la curva no se vea dentada
	sm := make([]float64, len(h))
	for i := range h {
		a, b := i-1, i+1
		if a < 0 {
			a = 0
		}
		if b >= len(h) {
			b = len(h) - 1
		}
		sm[i] = (h[a] + h[i]*2 + h[b]) / 4
	}
	if maxV <= 0 {
		for _, v := range sm {
			maxV = math.Max(maxV, v)
		}
		maxV *= 1.25
		if maxV <= 0 {
			maxV = 1
		}
	}
	gh := r.H * 0.36
	bottom := r.Y + r.H
	rad := gRadius(r)
	right := r.X + r.W - r.H*0.08 // el punto del valor actual queda dentro de la tarjeta
	step := (right - r.X) / float32(histLen-1)
	x0 := right - step*float32(len(sm)-1)
	pts := make([]PointF, len(sm))
	for i, v := range sm {
		f := float32(math.Max(0, math.Min(v/maxV, 1)))
		pts[i] = PointF{x0 + step*float32(i), bottom - gh*0.08 - f*gh*0.84}
	}
	c.ClipRoundRect(r.X, r.Y, r.W, r.H, rad)
	// guía al 50 %
	gy := bottom - gh*0.08 - gh*0.42
	for x := r.X + rad*0.6; x < r.X+r.W-rad*0.6; x += 10 {
		c.Line(x, gy, x+4, gy, 1, 0x14FFFFFF)
	}
	area := append([]PointF{{pts[0].X, bottom}}, pts...)
	area = append(area, PointF{right, bottom})
	c.FillPoly(area, col&0x00FFFFFF|0x2E000000)
	// franja más intensa pegada a la línea, para dar sensación de degradado
	band := make([]PointF, 0, len(pts)*2)
	band = append(band, pts...)
	for i := len(pts) - 1; i >= 0; i-- {
		band = append(band, PointF{pts[i].X, pts[i].Y + gh*0.12})
	}
	c.FillPoly(band, col&0x00FFFFFF|0x24000000)
	c.Polyline(pts, 2.2, col)
	c.ResetClip()
	last := pts[len(pts)-1]
	c.Circle(last.X, last.Y, 7, col&0x00FFFFFF|0x40000000)
	c.Circle(last.X, last.Y, 3.6, col)
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
		switch r {
		case 'á':
			out[i] = 'Á'
		case 'é':
			out[i] = 'É'
		case 'í':
			out[i] = 'Í'
		case 'ó':
			out[i] = 'Ó'
		case 'ú':
			out[i] = 'Ú'
		}
	}
	return string(out)
}

func tempCol(t float64) uint32 {
	if t >= 80 {
		return gWarn
	}
	return 0
}

func gPaintMonitor(c *Canvas, s PCStats) {
	top := gHeader(c, "Monitor del PC", "Se actualiza cada segundo")

	// CPU: uso grande, temperatura arriba a la derecha
	cpu := gStat{name: "CPU", value: fmt.Sprintf("%.0f", s.CPU), unit: "%", hist: s.HistCPU, histMax: 100, barFrac: -1, col: gColCPU, gauge: s.CPU}
	if s.CPUTemp >= 0 {
		cpu.right, cpu.rightCol = fmt.Sprintf("%.0f °C", s.CPUTemp), tempCol(s.CPUTemp)
	}
	cpu.line = s.CPUName
	if s.CPUMHz > 0 {
		cpu.line += fmt.Sprintf(" · %.2f GHz", s.CPUMHz/1000)
	}
	if s.CPU >= 85 {
		cpu.valueCol = gWarn
	}
	gStatCard(c, gMonRect(0, top), cpu)

	// GPU
	gpu := gStat{name: "GPU", value: fmt.Sprintf("%.0f", s.GPU), unit: "%", hist: s.HistGPU, histMax: 100, barFrac: -1, col: gColGPU, gauge: s.GPU}
	if s.GPU < 0 {
		gpu.value, gpu.unit = "—", ""
	}
	if s.GPUTemp >= 0 {
		gpu.right, gpu.rightCol = fmt.Sprintf("%.0f °C", s.GPUTemp), tempCol(s.GPUTemp)
	}
	gpu.line = s.GPUName
	if s.VRAMTotal > 0 {
		gpu.line += fmt.Sprintf(" · VRAM %.1f / %.0f GB", s.VRAMUsed, s.VRAMTotal)
	}
	if s.GPUPower >= 0 && s.GPUTemp < 0 {
		gpu.right = fmt.Sprintf("%.0f W", s.GPUPower)
	}
	if s.GPU >= 85 {
		gpu.valueCol = gWarn
	}
	gStatCard(c, gMonRect(1, top), gpu)

	// RAM
	ramPct := 0.0
	if s.RAMTotal > 0 {
		ramPct = s.RAMUsed / s.RAMTotal * 100
	}
	gStatCard(c, gMonRect(2, top), gStat{name: "RAM", right: fmt.Sprintf("%s / %.0f GB", fmtRAM(s.RAMUsed), math.Round(s.RAMTotal)),
		value: fmt.Sprintf("%.0f", ramPct), unit: "%", hist: s.HistRAM, histMax: 100, barFrac: -1, col: gColRAM, gauge: ramPct})

	// Disco
	diskFrac := 0.0
	if s.DiskTotal > 0 {
		diskFrac = 1 - s.DiskFree/s.DiskTotal
	}
	dv, du := splitUnit(fmtGB(s.DiskFree))
	gStatCard(c, gMonRect(3, top), gStat{name: "Disco C:", right: "de " + fmtGB(s.DiskTotal), value: dv, unit: du + " libres",
		line: fmt.Sprintf("Actividad %.0f %%", s.DiskAct), barFrac: diskFrac, col: gColDisk, gauge: -1})

	// Red
	nv, nu := splitUnit(fmtBytesRate(s.NetDown))
	gStatCard(c, gMonRect(4, top), gStat{name: "Red", right: "↑ " + fmtBytesRate(s.NetUp), value: "↓ " + nv, unit: nu,
		hist: s.HistNet, barFrac: -1, col: gColNet, gauge: -1})

	// Sistema
	h := int(s.Uptime.Hours())
	mnt := int(s.Uptime.Minutes()) % 60
	up := fmt.Sprintf("%d h %d min", h, mnt)
	if h >= 24 {
		up = fmt.Sprintf("%d d %d h", h/24, h%24)
	}
	now := time.Now()
	dias := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	meses := []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	gStatCard(c, gMonRect(5, top), gStat{name: "Sistema", right: fmt.Sprintf("%s %d de %s", dias[now.Weekday()], now.Day(), meses[now.Month()-1]),
		value: now.Format("15:04"), line: "Encendido hace " + up, barFrac: -1, col: gColSys, gauge: -1})
}

// ---------------------------------------------------------------- mezclador

func gMixTop() float32 { return pad*1.4 + panelH*0.037*1.6 + pad*0.9 }

func gMixCols() int {
	n := len(mixApps)
	if n > maxMixApps {
		n = maxMixApps
	}
	if n < 4 {
		n = 4 // con pocas apps las columnas no se estiran de más
	}
	return n
}

func gMixCard(i int) RectF {
	cols := gMixCols()
	top := gMixTop()
	bottom := pad*1.2 + 10
	cw := (panelW - pad*float32(cols+1)) / float32(cols)
	return RectF{pad + float32(i)*(cw+pad), top, cw, panelH - top - bottom}
}

// medidas internas de una columna del mezclador
func gMixParts(i int) (r RectF, p, chip, nameY, faderY, faderH, pctY, muteY, muteH float32) {
	r = gMixCard(i)
	p = r.W * 0.08
	chip = r.W * 0.27
	nameY = r.Y + p + chip + p*0.7
	muteH = r.H * 0.095
	muteY = r.Y + r.H - p - muteH
	pctY = muteY - p*0.6 - r.H*0.06
	faderY = nameY + r.H*0.07
	faderH = pctY - p*0.5 - faderY
	return
}

func gFaderRect(i int) RectF {
	r, _, _, _, fy, fh, _, _, _ := gMixParts(i)
	w := r.W * 0.36
	return RectF{r.X + (r.W-w)/2, fy, w, fh}
}

func gMuteRect(i int) RectF {
	r, p, _, _, _, _, _, my, mh := gMixParts(i)
	return RectF{r.X + p, my, r.W - 2*p, mh}
}

func gPaintMixer(c *Canvas) {
	n := len(mixApps)
	if n > maxMixApps {
		n = maxMixApps
	}
	right := gDeviceName(aud.DeviceName)
	if right != "" {
		right += " · "
	}
	if n == 1 {
		right += "1 app con sonido"
	} else {
		right += itoa(n) + " apps con sonido"
	}
	gHeader(c, "Mezclador", right)
	for i := 0; i < n; i++ {
		a := mixApps[i]
		r, p, chip, nameY, _, _, pctY, _, _ := gMixParts(i)
		bg := uint32(gSurface)
		if mixDrag == i {
			bg = lighten(bg, 0.04)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, min32(r.W, r.H)*0.075, bg)
		// ícono de la app
		cx := r.X + (r.W-chip)/2
		cy := r.Y + p
		c.RoundRect(cx, cy, chip, chip, chip*0.18, gRaised)
		if a.system {
			is := chip * 0.5
			gIcon(c, nil, glyphs["speaker_high_volume"], cx+(chip-is)/2, cy+(chip-is)/2, is, gText)
		} else if img, _ := appIcon(a.exe); img != nil {
			is := chip * 0.62
			c.DrawImage(img, cx+(chip-is)/2, cy+(chip-is)/2, is, is)
		} else {
			c.Text(upper(string([]rune(a.name)[:1])), semiFont, chip*0.42, semiStyle, gText, RectF{cx, cy, chip, chip}, AlignCenter, AlignCenter)
		}
		fs := r.W * 0.082
		c.Text(a.name, semiFont, fs, semiStyle, gText, RectF{r.X + p*0.5, nameY, r.W - p, fs * 1.5}, AlignCenter, AlignCenter)

		// barra vertical: el relleno sube desde abajo
		f := gFaderRect(i)
		rad := f.W * 0.32
		c.RoundRect(f.X, f.Y, f.W, f.H, rad, gRaised)
		fill := uint32(gAccent)
		if a.muted {
			fill = 0xFF4A4F59
		}
		fh := f.H * a.volume
		if fh > 1 {
			c.RoundRect(f.X, f.Y+f.H-fh, f.W, fh, min32(rad, fh/2), fill)
		}
		// nivel de lo que suena ahora: una marca fina dentro del relleno
		if a.peak > 0.005 && !a.muted && fh > 8 {
			ph := fh * float32(math.Min(1, float64(a.peak)*1.4))
			y := f.Y + f.H - ph
			c.RoundRect(f.X+f.W*0.25, y, f.W*0.5, 3, 1.5, 0x590B0C0F)
		}
		// porcentaje
		pct := fmt.Sprintf("%d %%", int(math.Round(float64(a.volume)*100)))
		if a.muted {
			pct = "Silencio"
		}
		c.Text(pct, semiFont, fs, semiStyle, gText, RectF{r.X, pctY, r.W, r.H * 0.06}, AlignCenter, AlignCenter)
		// botón silenciar / activar
		m := gMuteRect(i)
		mb, mt, label := uint32(gRaised), uint32(gText), "Silenciar"
		g := glyphs["speaker_high_volume"]
		if a.muted {
			mb, mt, label, g = gText, gInk, "Activar", glyphs["muted_speaker"]
		}
		if pressed == 2100+i {
			mb = lighten(mb, 0.1)
		}
		c.RoundRect(m.X, m.Y, m.W, m.H, m.H*0.3, mb)
		ms := m.H * 0.34
		tw := textWidth(c, label, semiFont, ms, semiStyle)
		total := ms*1.25 + tw
		x := m.X + (m.W-total)/2
		gIcon(c, nil, g, x, m.Y+(m.H-ms)/2, ms, mt)
		c.Text(label, semiFont, ms, semiStyle, mt, RectF{x + ms*1.25, m.Y, tw + ms*0.7, m.H}, AlignNear, AlignCenter)
	}
}

// ---------------------------------------------------------------- avisos

// gPaintNotif: aviso de WhatsApp (o de actualización) como tarjeta grafito
// elevada; el color queda solo en la pastilla del ícono.
func gPaintNotif(c *Canvas, r RectF) {
	rad := r.H * 0.24
	c.RoundRect(r.X, r.Y+r.H*0.05, r.W, r.H, rad, 0x8C000000) // sombra
	bg := uint32(0xFF2A2E35)
	if notifPressed {
		bg = lighten(bg, 0.06)
	}
	c.RoundRect(r.X, r.Y, r.W, r.H, rad, bg)
	c.RoundRectStroke(r.X+0.5, r.Y+0.5, r.W-1, r.H-1, rad, 1, 0xFF3A3F48)
	chip := r.H * 0.56
	cx, cy := r.X+r.H*0.22, r.Y+(r.H-chip)/2
	tint, g := uint32(gGreen), glyphs["speech_balloon"]
	if isUpdateBanner() {
		tint, g = gAccent, 0xE896 // Download
	}
	c.RoundRect(cx, cy, chip, chip, chip*0.3, tint)
	is := chip * 0.5
	gIcon(c, nil, g, cx+(chip-is)/2, cy+(chip-is)/2, is, gInk)
	tx := cx + chip + r.H*0.2
	tw := r.X + r.W - tx - r.H*0.3
	if notifMore > 0 {
		mw := r.H * 0.55
		tw -= mw
		mr := RectF{r.X + r.W - r.H*0.25 - mw, r.Y + (r.H-r.H*0.36)/2, mw, r.H * 0.36}
		c.RoundRect(mr.X, mr.Y, mr.W, mr.H, mr.H/2, gRaised)
		c.Text("+"+itoa(min(notifMore, 99)), semiFont, r.H*0.17, semiStyle, gText, mr, AlignCenter, AlignCenter)
	}
	body := notifCur.Body
	if !isUpdateBanner() && (!cfg.WhatsAppTexto || body == "") {
		body = "Nuevo mensaje"
	}
	c.Text(notifCur.Title, semiFont, r.H*0.21, semiStyle, gText, RectF{tx, r.Y + r.H*0.15, tw, r.H * 0.36}, AlignNear, AlignCenter)
	c.Text(body, textFont, r.H*0.17, FontStyleRegular, gMuted, RectF{tx, r.Y + r.H*0.5, tw, r.H * 0.34}, AlignNear, AlignCenter)
}

// ---------------------------------------------------------------- reposo

// idleClockFont: letra fina para el reloj del reposo (la que haya instalada).
var idleClockFont string

func clockFont() string {
	if idleClockFont == "" {
		idleClockFont = textFont
		for _, f := range []string{"Segoe UI Variable Display Light", "Segoe UI Variable Display Semil", "Segoe UI Light"} {
			if fontExists(f) {
				idleClockFont = f
				break
			}
		}
	}
	return idleClockFont
}

// gIdleBackground: con música, la carátula muy difuminada y oscura de fondo;
// sin música, luces de color suaves que se mueven muy lento.
func gIdleBackground(c *Canvas, now time.Time) {
	t := float64(now.UnixMilli()%600000) / 600000 * 2 * math.Pi // una vuelta cada 10 min
	if mediaShowing() && mediaAmbient != nil {
		// la imagen de 7×7 estirada queda como un desenfoque; se mueve un poco
		ox := float32(math.Sin(t)) * panelW * 0.04
		oy := float32(math.Cos(t*0.7)) * panelH * 0.04
		c.drawImageHQ(mediaAmbient, -panelW*0.15+ox, -panelH*0.25+oy, panelW*1.3, panelH*1.5)
		c.RoundRect(-2, -2, panelW+4, panelH+4, 0, 0x9E000000)
		c.RoundRectGrad(-2, panelH*0.45, panelW+4, panelH*0.57, 0, 0x00000000, 0xA0000000)
		return
	}
	c.drawImageHQ(idleAurora(t), -2, -2, panelW+4, panelH+4)
}

var (
	auroraImg *Image
	auroraAt  time.Time
)

// idleAurora: imagen chica (64×38) con tres luces de color calculadas píxel a
// píxel; estirada queda como un degradado suave, sin anillos. Se recalcula a
// lo más una vez por segundo.
func idleAurora(t float64) *Image {
	if auroraImg != nil && time.Since(auroraAt) < time.Second {
		return auroraImg
	}
	const w, h = 64, 38
	blobs := []struct {
		x, y, r    float64
		cr, cg, cb float64
	}{
		{0.22 + 0.07*math.Sin(t), 0.32 + 0.06*math.Cos(t*1.3), 0.30, 58, 75, 214},
		{0.80 + 0.06*math.Cos(t*0.8), 0.26 + 0.07*math.Sin(t*1.1), 0.26, 122, 63, 208},
		{0.55 + 0.08*math.Sin(t*0.6+1), 0.86 + 0.05*math.Cos(t), 0.30, 22, 140, 136},
	}
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := 9.0, 10.0, 14.0 // casi negro
			fx, fy := float64(x)/(w-1), float64(y)/(h-1)
			for _, bl := range blobs {
				dx, dy := fx-bl.x, (fy-bl.y)*float64(h)/float64(w)*1.6
				a := 0.42 * math.Exp(-(dx*dx+dy*dy)/(2*bl.r*bl.r*0.36))
				r += (bl.cr - r) * a
				g += (bl.cg - g) * a
				b += (bl.cb - b) * a
			}
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = clampByte(r), clampByte(g), clampByte(b), 255
		}
	}
	auroraImg, auroraAt = newImage(w, h, pix), time.Now()
	return auroraImg
}

// gPaintIdle: pantalla de reposo. Con música: reloj y fecha a la izquierda y
// la canción a la derecha, con su carátula grande sobre su propio color. Sin
// música: el reloj al centro sobre luces suaves.
func gPaintIdle(c *Canvas) {
	a := uint32(255 * math.Min(1, float64(idleAnim)))
	c.ResetTransform()
	c.RoundRect(-2, -2, panelW+4, panelH+4, 0, a<<24) // negro encima
	if idleAnim < 0.6 {
		return
	}
	ta := uint32(255 * math.Min(1, float64(idleAnim-0.6)/0.4))
	col := func(c uint32) uint32 { return (c & 0x00FFFFFF) | (ta*((c>>24)&0xFF)/255)<<24 }
	now := time.Now()
	gIdleBackground(c, now)
	if ta < 255 {
		c.RoundRect(-2, -2, panelW+4, panelH+4, 0, (255-ta)<<24) // el fondo aparece de a poco
	}
	// pequeño desplazamiento cada minuto para no marcar la pantalla
	dx := float32(math.Sin(float64(now.Minute()))) * panelW * 0.02
	dy := float32(math.Cos(float64(now.Minute()))) * panelH * 0.02

	dias := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	meses := []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	date := upper(fmt.Sprintf("%s %d de %s", dias[now.Weekday()], now.Day(), meses[now.Month()-1]))
	show := mediaShowing()

	// reloj, fecha y mensajes: un bloque que va a la izquierda (con música) o al centro
	clockS := panelH * 0.30
	if !show {
		clockS = panelH * 0.34
	}
	ds := panelH * 0.03
	blockH := clockS*1.05 + ds*2.2
	if notifPending() > 0 {
		blockH += panelH * 0.09
	}
	bx, bw, align := panelW*0.075+dx, panelW*0.46, AlignNear
	if !show {
		bx, bw, align = dx, panelW, AlignCenter
	}
	y := (panelH-blockH)/2 + dy
	c.Text(now.Format("15:04"), clockFont(), clockS, FontStyleRegular, col(0xFFF2F3F5), RectF{bx - clockS*0.04, y, bw, clockS * 1.05}, align, AlignCenter)
	y += clockS * 1.05
	c.Text(date, semiFont, ds, semiStyle, col(0xCCFFFFFF), RectF{bx, y, bw, ds * 1.6}, align, AlignCenter)
	y += ds * 2.2
	if n := notifPending(); n > 0 {
		t := "1 mensaje de WhatsApp"
		if n > 1 {
			t = itoa(n) + " mensajes de WhatsApp"
		}
		ms := panelH * 0.026
		tw := textWidth(c, t, textFont, ms, FontStyleRegular)
		pw, ph := tw+ms*2.6, ms*2.2
		px := bx
		if !show {
			px = (panelW-pw)/2 + dx
		}
		py := y + panelH*0.02
		c.RoundRect(px, py, pw, ph, ph/2, col(0x22FFFFFF))
		c.Circle(px+ms*0.95, py+ph/2, ms*0.3, col(gGreen))
		c.Text(t, textFont, ms, FontStyleRegular, col(0xE6FFFFFF), RectF{px + ms * 1.6, py, tw + ms*0.8, ph}, AlignNear, AlignCenter)
	}

	if show {
		// la canción: carátula grande, título, artista y progreso
		art := panelH * 0.42
		cx := panelW*0.73 + dx
		ax, ay := cx-art/2, (panelH-art-panelH*0.2)/2+dy
		c.RoundRect(ax, ay+art*0.03, art, art, art*0.06, col(0x66000000)) // sombra
		if mediaThumb != nil {
			c.ClipRoundRect(ax, ay, art, art, art*0.06)
			c.drawImageHQ(mediaThumb, ax, ay, art, art)
			c.ResetClip()
			if ta < 255 {
				c.RoundRect(ax, ay, art, art, art*0.06, (255-ta)<<24)
			}
		} else {
			c.RoundRect(ax, ay, art, art, art*0.06, col(0x33FFFFFF))
		}
		tw := art * 1.25
		ty := ay + art + panelH*0.035
		ts := panelH * 0.036
		c.Text(sinEmojis(media.Title), semiFont, ts, semiStyle, col(0xFFFFFFFF), RectF{cx - tw/2, ty, tw, ts * 1.45}, AlignCenter, AlignCenter)
		sub := sinEmojis(media.Artist)
		if media.Status != mediaPlaying {
			sub = "En pausa · " + sub
		}
		c.Text(sub, textFont, ts*0.75, FontStyleRegular, col(0xB3FFFFFF), RectF{cx - tw/2, ty + ts*1.4, tw, ts * 1.3}, AlignCenter, AlignCenter)
		if media.End > 0 {
			pos := media.Pos
			if media.Status == mediaPlaying && media.Updated > 0 {
				pos += nowFiletime() - media.Updated
			}
			f := float32(math.Min(1, float64(pos)/float64(media.End)))
			bw, bh := art, panelH*0.008
			by := ty + ts*3.1
			c.RoundRect(ax, by, bw, bh, bh/2, col(0x33FFFFFF))
			if f > 0 {
				c.RoundRect(ax, by, bw*f, bh, bh/2, col(0xFF000000|gAlbumColor()&0xFFFFFF))
			}
		}
	}

	hs := panelH * 0.022
	c.Text("Toca para volver", textFont, hs, FontStyleRegular, col(0x66FFFFFF), RectF{0, panelH - hs*3, panelW, hs * 1.6}, AlignCenter, AlignCenter)
}

// notifPending: mensajes de WhatsApp que llegaron y siguen en el aviso.
func notifPending() int {
	if !notifShown || isUpdateBanner() {
		return 0
	}
	return 1 + notifMore
}

var gMeasureString = gdiplus.NewProc("GdipMeasureString")

// textWidth mide el ancho de un texto de una línea, en píxeles.
func textWidth(c *Canvas, s, familyName string, sizePx float32, style int) float32 {
	if s == "" {
		return 0
	}
	fam := c.family(familyName)
	if fam == 0 {
		fam = c.family("Segoe UI")
	}
	if fam == 0 {
		return sizePx * 0.55 * float32(len([]rune(s)))
	}
	font := cachedFont(fam, sizePx, style)
	layout := RectF{0, 0, 10000, sizePx * 3}
	var box RectF
	gMeasureString.Call(c.g, wstrPtr(s), ^uintptr(0), font, uintptr(unsafe.Pointer(&layout)),
		typographicFormat(), uintptr(unsafe.Pointer(&box)), 0, 0)
	return box.W
}

// splitUnit separa "312 GB" en "312" y "GB".
func splitUnit(s string) (string, string) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ' ' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

var (
	gTypographic    = gdiplus.NewProc("GdipStringFormatGetGenericTypographic")
	gTypographicFmt uintptr
)

// typographicFormat: formato sin el margen extra que GDI+ agrega al medir.
func typographicFormat() uintptr {
	if gTypographicFmt == 0 {
		gTypographic.Call(uintptr(unsafe.Pointer(&gTypographicFmt)))
	}
	return gTypographicFmt
}
