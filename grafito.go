//go:build windows

package main

// Tema "Grafito": minimalista. Fondo casi negro, tarjetas grafito planas,
// íconos de línea (Segoe Fluent Icons) y un solo color de acento para lo
// importante (reproducir, volumen). Mismas funciones, solo cambia el dibujo.

import (
	"strings"
	"time"
)

const (
	// Grises más claros que en el prototipo: en los mini monitores los tonos
	// muy cercanos al negro se ven negros del todo.
	gBG      = 0xFF0B0C0F
	gSurface = 0xFF23262D
	gRaised  = 0xFF30343C
	gTrack   = 0xFF3A3F48
	gText    = 0xFFEDEEF1
	gMuted   = 0xFF8B909A
	gAccent  = 0xFFA9B4FF
	gInk     = 0xFF0B0C0F // texto sobre el acento
	gRed     = 0xFFFF8A8A
	gGreen   = 0xFF5BD68A
)

func isGrafito() bool { return cfg.Theme == "grafito" }

func gRadius(r RectF) float32 { return min32(r.W, r.H) * 0.14 }

// glyphs: ícono de línea equivalente a cada emoji. Si un nombre no está, se
// usa la imagen de siempre.
var glyphs = map[string]rune{
	"speaker_high_volume":          0xE767,
	"speaker_medium_volume":        0xE994,
	"speaker_low_volume":           0xE993,
	"muted_speaker":                0xE74F,
	"studio_microphone":            0xE720,
	"last_track_button":            0xE892,
	"next_track_button":            0xE893,
	"play_or_pause_button":         0xE768,
	"headphone":                    0xE7F6,
	"camera_with_flash":            0xE722,
	"camera":                       0xE722,
	"open_file_folder":             0xE8B7,
	"card_index_dividers":          0xE8B7,
	"cross_mark":                   0xE711,
	"crescent_moon":                0xE708,
	"stop_sign":                    0xE7E8,
	"gear":                         0xE713,
	"abacus":                       0xE8EF,
	"bar_chart":                    0xE9D9,
	"desktop_computer":             0xE7F4,
	"television":                   0xE7F4,
	"clipboard":                    0xE77F,
	"grinning_face":                0xE76E,
	"locked":                       0xE72E,
	"keyboard":                     0xE765,
	"globe_with_meridians":         0xE774,
	"memo":                         0xE70B,
	"page_facing_up":               0xE8A5,
	"right_arrow":                  0xE72A,
	"musical_note":                 0xEC4F,
	"link":                         0xE71B,
	"envelope":                     0xE715,
	"calendar":                     0xE787,
	"bell":                         0xEA8F,
	"house":                        0xE80F,
	"magnifying_glass_tilted_left": 0xE721,
	"light_bulb":                   0xEA80,
	"star":                         0xE734,
	"red_heart":                    0xEB51,
	"shopping_cart":                0xE7BF,
	"briefcase":                    0xE821,
	"cloud":                        0xE753,
	"film_frames":                  0xE714,
	"speech_balloon":               0xE8BD,
	"hammer_and_wrench":            0xE90F,
	"alarm_clock":                  0xE823,
	"hourglass_not_done":           0xE916,
	"thumbs_up":                    0xE8E1,
	"laptop":                       0xE7F8,
	"mobile_phone":                 0xE8EA,
	"video_game":                   0xE7FC,
	"joystick":                     0xE7FC,
	"robot":                        0xE99A,
	"artist_palette":               0xE790,
}

const (
	glyphPause  = 0xE769
	glyphMicOff = 0xEC54
	glyphAdd    = 0xE710
)

func glyphOf(img *Image) (rune, bool) {
	if img == nil || img.name == "" {
		return 0, false
	}
	g, ok := glyphs[img.name]
	return g, ok
}

// gIcon dibuja el ícono como glifo de línea (o la imagen si no hay glifo).
// x, y es la esquina superior izquierda; s el tamaño.
func gIcon(c *Canvas, img *Image, g rune, x, y, s float32, col uint32) {
	if g == 0 {
		if gg, ok := glyphOf(img); ok {
			g = gg
		}
	}
	if g != 0 {
		c.Text(string(g), iconFont, s*0.92, FontStyleRegular, col, RectF{x - s*0.2, y - s*0.2, s * 1.4, s * 1.4}, AlignCenter, AlignCenter)
		return
	}
	if img != nil {
		c.drawImageHQ(img, x, y, s, s)
	}
}

// gCard dibuja el fondo de un botón y devuelve su rectángulo animado.
func gCard(c *Canvas, i int, bg uint32) RectF {
	r := visualRect(i)
	if pa := pressAnim[i]; pa > 0.01 {
		bg = lighten(bg, 0.17*pa) // al tocar se aclara (y se hunde, ver visualRect)
	}
	c.RoundRect(r.X, r.Y, r.W, r.H, gRadius(r), bg)
	drawRipples(c, i, r, gRadius(r))
	return r
}

// gLabel: texto abajo a la izquierda. La línea tenue (sub) va encima del
// título, como en el diseño ("Salida" / "Altavoces").
func gLabel(c *Canvas, r RectF, title, sub string, titleCol uint32) {
	base := min32(r.W, r.H)
	p := base * 0.115
	fs := base * 0.1
	ss := base * 0.08
	y := r.Y + r.H - p - fs*1.4
	if sub == "" {
		// sin línea tenue el título puede ocupar dos líneas
		c.TextWrap(title, semiFont, fs, semiStyle, titleCol, RectF{r.X + p, r.Y + r.H - p - fs*2.7, r.W - 2*p, fs * 2.7}, AlignNear, AlignFar)
		return
	}
	c.Text(title, semiFont, fs, semiStyle, titleCol, RectF{r.X + p, y, r.W - 2*p, fs * 1.4}, AlignNear, AlignCenter)
	c.Text(sub, textFont, ss, FontStyleRegular, gMuted, RectF{r.X + p, y - ss*1.35, r.W - 2*p, ss * 1.35}, AlignNear, AlignCenter)
}

// Formas rellenas para los controles de música (como en el diseño).
const (
	shapePrev = iota
	shapeNext
	shapePlay
	shapePause
)

func gShape(c *Canvas, kind int, cx, cy, s float32, col uint32) {
	h := s / 2
	switch kind {
	case shapePause:
		bw := s * 0.24
		c.RoundRect(cx-h*0.62, cy-h*0.8, bw, s*0.8, bw*0.22, col)
		c.RoundRect(cx+h*0.62-bw, cy-h*0.8, bw, s*0.8, bw*0.22, col)
	case shapePlay:
		c.FillPoly([]PointF{{cx - h*0.62, cy - h*0.86}, {cx + h*0.86, cy}, {cx - h*0.62, cy + h*0.86}}, col)
	case shapePrev:
		c.RoundRect(cx-h*0.8, cy-h*0.78, s*0.12, s*0.78, s*0.02, col)
		c.FillPoly([]PointF{{cx + h*0.8, cy - h*0.78}, {cx - h*0.5, cy}, {cx + h*0.8, cy + h*0.78}}, col)
	case shapeNext:
		c.RoundRect(cx+h*0.8-s*0.12, cy-h*0.78, s*0.12, s*0.78, s*0.02, col)
		c.FillPoly([]PointF{{cx - h*0.8, cy - h*0.78}, {cx + h*0.5, cy}, {cx - h*0.8, cy + h*0.78}}, col)
	}
}

// gTile: botón de la página principal con ícono arriba y texto abajo.
func gTile(c *Canvas, i int, img *Image, g rune, iconCol uint32, title, sub string, titleCol uint32) RectF {
	r := gCard(c, i, gSurface)
	base := min32(r.W, r.H)
	p := base * 0.115
	s := base * 0.23
	gIcon(c, img, g, r.X+p, r.Y+p, s, iconCol)
	if title != "" {
		gLabel(c, r, title, sub, titleCol)
	}
	return r
}

// gCenterTile: botón con una forma rellena al centro (anterior / play / siguiente).
func gCenterTile(c *Canvas, i int, shape int, bg, col uint32) {
	r := gCard(c, i, bg)
	s := min32(r.W, r.H) * 0.21
	if shape == shapePlay || shape == shapePause {
		s = min32(r.W, r.H) * 0.24
	}
	gShape(c, shape, r.X+r.W/2, r.Y+r.H/2, s, col)
}

func paintPage1Grafito(c *Canvas) {
	// Sonido: arriba "Sonido", abajo el estado (como "Salida" / nombre)
	if aud.Muted {
		gTile(c, tSpeaker, nil, glyphs["muted_speaker"], gRed, "Silenciado", "Sonido", gRed)
	} else {
		vol := itoa(int(aud.Volume*100+0.5)) + " %"
		gTile(c, tSpeaker, nil, glyphs["speaker_high_volume"], gText, vol, "Sonido", gText)
	}

	tv := time.Now()
	drawVolume(c)
	dbgAdd("vol", tv)

	// Micrófono: punto verde abierto, rojo silenciado
	if aud.MicMuted {
		r := gTile(c, tMic, nil, glyphMicOff, gRed, "Silenciado", "Micrófono", gRed)
		c.Circle(r.X+r.W-r.H*0.15, r.Y+r.H*0.17, r.H*0.035, gRed)
	} else {
		r := gTile(c, tMic, nil, glyphs["studio_microphone"], gText, "Activo", "Micrófono", gText)
		c.Circle(r.X+r.W-r.H*0.15, r.Y+r.H*0.17, r.H*0.035, gGreen)
	}

	// Multimedia: reproducir es el único botón con color
	gCenterTile(c, tPrev, shapePrev, gSurface, gText)
	if playPaused {
		gCenterTile(c, tPlay, shapePlay, gAccent, gInk)
	} else {
		gCenterTile(c, tPlay, shapePause, gAccent, gInk)
	}
	gCenterTile(c, tNext, shapeNext, gSurface, gText)

	// Salida de audio
	if hcap := flashText(tHeadphones); hcap != "" {
		gTile(c, tHeadphones, nil, glyphs["headphone"], gText, hcap, "", gText)
	} else {
		gTile(c, tHeadphones, nil, glyphs["headphone"], gText, gDeviceName(aud.DeviceName), "Salida", gText)
	}

	for i := 0; i < len(mainIDs); i++ {
		drawSlot(c, -1, i)
	}
}

// ---------------------------------------------------------------- música

// gMediaGeom: medidas de la tarjeta de música en Grafito: carátula grande a
// la izquierda (todo el alto) y una columna de texto y barras a la derecha.
func gMediaGeom(r RectF) (p, art, ax, ay, tx float32) {
	p = r.H * 0.09
	art = r.H - 2*p
	ax, ay = r.X+p, r.Y+p
	tx = ax + art + p*1.15
	return
}

// gVolStrip: franja del volumen, abajo de la columna de texto.
func gVolStrip(r RectF) RectF {
	p, _, _, _, tx := gMediaGeom(r)
	h := r.H * 0.2
	x := tx - h*0.32
	return RectF{x, r.Y + r.H - p - h, r.X + r.W - p*0.6 - x, h}
}

func gProgressY(r RectF) float32 { return r.Y + r.H*0.565 }

func gMediaStatic(c *Canvas, r RectF) {
	_, art, ax, ay, tx := gMediaGeom(r)
	rad := art * 0.11
	if mediaThumb != nil {
		c.RoundRect(ax, ay, art, art, rad, gRaised) // por si la carátula es transparente
		c.ClipRoundRect(ax, ay, art, art, rad)
		c.drawImageHQ(mediaThumb, ax, ay, art, art)
		c.ResetClip()
	} else {
		c.RoundRect(ax, ay, art, art, rad, gRaised)
		is := art * 0.3
		gIcon(c, nil, glyphs["musical_note"], ax+(art-is)/2, ay+(art-is)/2, is, gMuted)
	}
	if media.End > 0 {
		th := r.H * 0.035
		c.RoundRect(tx, gProgressY(r)-th/2, r.X+r.W-r.H*0.09-tx, th, th/2, gTrack)
	}
}

func gMediaHeader(c *Canvas, r RectF, rightLimit float32) {
	p, _, _, ay, tx := gMediaGeom(r)
	tw := rightLimit - tx
	app := strings.TrimSuffix(strings.TrimSuffix(media.App, ".exe"), ".EXE")
	if i := strings.LastIndexAny(app, `\/!`); i >= 0 {
		app = app[i+1:]
	}
	if app == "" {
		app = "Música"
	}
	state, titleCol := " · Sonando", uint32(gText)
	if media.Status != mediaPlaying {
		state, titleCol = " · En pausa", gMuted
	}
	ls := r.H * 0.076
	c.Text(strings.ToUpper(app+state), semiFont, ls, semiStyle, gAccent, RectF{tx, ay - ls*0.15, tw, ls * 1.4}, AlignNear, AlignCenter)
	fs := r.H * 0.135
	ty := ay + ls*1.3
	c.Text(sinEmojis(media.Title), semiFont, fs, semiStyle, titleCol, RectF{tx, ty, tw, fs * 1.35}, AlignNear, AlignCenter)
	as := r.H * 0.095
	c.Text(sinEmojis(media.Artist), textFont, as, FontStyleRegular, gMuted, RectF{tx, ty + fs*1.25, tw, as * 1.4}, AlignNear, AlignCenter)

	if media.End <= 0 {
		return
	}
	pos := media.Pos
	if media.Status == mediaPlaying && media.Updated > 0 {
		pos += nowFiletime() - media.Updated
	}
	if pos > media.End {
		pos = media.End
	}
	frac := float32(pos) / float32(media.End)
	px0, px1 := tx, r.X+r.W-p
	py := gProgressY(r)
	th := r.H * 0.035
	seekBarX0, seekBarX1 = px0, px1
	seekBar = RectF{px0 - as*0.5, py - as*1.0, px1 - px0 + as, as * 2.0}
	if seeking {
		frac = seekFrac
		pos = int64(float64(seekFrac) * float64(media.End))
		th *= 1.8
		c.RoundRect(px0, py-th/2, px1-px0, th, th/2, gTrack)
	}
	if frac > 0 {
		c.RoundRect(px0, py-th/2, (px1-px0)*frac, th, th/2, gText)
	}
	if seeking {
		c.Circle(px0+(px1-px0)*frac, py, th*1.4, gText)
	}
	ts := r.H * 0.08
	tr := RectF{px0, py + th*0.7, px1 - px0, ts * 1.45}
	c.Text(fmtTime(pos), textFont, ts, FontStyleRegular, gMuted, tr, AlignNear, AlignCenter)
	c.Text(fmtTime(media.End), textFont, ts, FontStyleRegular, gMuted, tr, AlignFar, AlignCenter)
}

// kindOf: qué hace un botón, en pocas palabras (línea tenue bajo el nombre).
func kindOf(b BotonP2) string {
	switch b.Tipo {
	case "app":
		return "App"
	case "file":
		return "Archivo"
	case "media":
		return "Multimedia"
	case "hotkey":
		return "Atajo"
	case "url":
		return "Web"
	case "text":
		return "Texto"
	case "page":
		return "Ir a página"
	}
	return "Sistema"
}

// gDeviceName acorta el nombre de la salida para que quepa en el botón
// ("Altavoces (ATK N9 PRO dongle)" → "ATK N9 PRO").
func gDeviceName(n string) string {
	n = ShortName(n)
	for _, w := range []string{" dongle", " Dongle", " Wireless", " USB"} {
		n = strings.Replace(n, w, "", 1)
	}
	return strings.TrimSpace(n)
}

// gPastel: color de botón llevado a un tono claro y vivo para la pastilla del ícono.
func gPastel(col uint32) uint32 {
	c := vivid(vivid(col))
	r, g, b := float32((c>>16)&0xFF), float32((c>>8)&0xFF), float32(c&0xFF)
	mx := r
	if g > mx {
		mx = g
	}
	if b > mx {
		mx = b
	}
	if mx < 1 {
		mx = 1
	}
	k := 235 / mx
	c = 0xFF000000 | uint32(r*k)<<16 | uint32(g*k)<<8 | uint32(b*k)
	return mix(c, 0xFFFFFFFF, 0.3)
}

// gPreviewCard: vista previa del botón en el editor, igual que en el panel.
func gPreviewCard(c *Canvas, r RectF, img *Image, label, sub string, col uint32) {
	c.RoundRect(r.X, r.Y, r.W, r.H, min32(r.W, r.H)*0.14, gSurface)
	base := min32(r.W, r.H)
	p := base * 0.11
	gChip(c, img, r.X+p, r.Y+p, base*0.3, col)
	fs := base * 0.1
	ss := base * 0.068
	y := r.Y + r.H - p - ss*1.4
	c.Text(sub, textFont, ss, FontStyleRegular, gMuted, RectF{r.X + p, y, r.W - 2*p, ss * 1.4}, AlignNear, AlignCenter)
	c.TextWrap(label, semiFont, fs, semiStyle, gText, RectF{r.X + p, y - fs*2.6, r.W - 2*p, fs * 2.6}, AlignNear, AlignFar)
}

// gOptionTile: una opción de la cuadrícula del editor.
func gOptionTile(c *Canvas, r RectF, s float32, img *Image, label string, col uint32, sel, pressed bool) {
	bg := uint32(gSurface)
	if sel {
		bg = gRaised
	}
	if pressed {
		bg = lighten(bg, 0.12)
	}
	rad := 14 * s
	c.RoundRect(r.X, r.Y, r.W, r.H, rad, bg)
	if sel {
		c.RoundRectStroke(r.X+1, r.Y+1, r.W-2, r.H-2, rad, 2.5*s, gAccent)
	}
	chip := 40 * s
	if img != nil {
		gChip(c, img, r.X+(r.W-chip)/2, r.Y+12*s, chip, col)
	}
	c.TextWrap(label, textFont, 12*s, FontStyleRegular, gText, RectF{r.X + 6*s, r.Y + 58*s, r.W - 12*s, 36 * s}, AlignCenter, AlignNear)
}

// gChip: pastilla de color con el ícono de línea adentro (o la imagen de la
// app sobre gris si no hay glifo).
func gChip(c *Canvas, img *Image, x, y, chip float32, col uint32) {
	if _, ok := glyphOf(img); ok {
		c.RoundRect(x, y, chip, chip, chip*0.32, gPastel(col))
		is := chip * 0.5
		gIcon(c, img, 0, x+(chip-is)/2, y+(chip-is)/2, is, gInk)
	} else if img != nil {
		c.RoundRect(x, y, chip, chip, chip*0.32, gRaised)
		is := chip * 0.66
		c.drawImageHQ(img, x+(chip-is)/2, y+(chip-is)/2, is, is)
	}
}

// gSlot dibuja un botón configurable en el estilo Grafito.
func gSlot(c *Canvas, bp, s int, img *Image, caption string, col uint32) {
	id := slotID(bp, s)
	b := slotCfg(bp, s)
	empty := b.Tipo == "action" && b.Accion == "empty" && b.Nombre == ""
	iconCol := uint32(gText)
	if b.Tipo == "action" && b.Accion == "shutdown" {
		iconCol = gRed
	}
	if bp < 0 {
		// fila de abajo de la página principal: igual que los demás botones
		gTile(c, id, img, 0, iconCol, caption, "", gText)
		return
	}
	bg := uint32(gSurface)
	if reorderID >= 0 && hitTest(lastPX, lastPY) == id {
		bg = gRaised
	}
	r := gCard(c, id, bg)
	base := min32(r.W, r.H)
	p := base * 0.095
	if empty {
		is := base * 0.14
		gIcon(c, nil, glyphAdd, r.X+(r.W-is)/2, r.Y+(r.H-is)/2, is, 0xFF4A4F59)
		return
	}
	// ícono dentro de una pastilla con el color del botón
	chip := base * 0.27
	tint := gPastel(col)
	if _, ok := glyphOf(img); ok {
		c.RoundRect(r.X+p, r.Y+p, chip, chip, chip*0.32, tint)
		is := chip * 0.5
		ic := uint32(gInk)
		gIcon(c, img, 0, r.X+p+(chip-is)/2, r.Y+p+(chip-is)/2, is, ic)
	} else if img != nil {
		c.RoundRect(r.X+p, r.Y+p, chip, chip, chip*0.32, gRaised)
		is := chip * 0.66
		c.drawImageHQ(img, r.X+p+(chip-is)/2, r.Y+p+(chip-is)/2, is, is)
	}
	// "Mantener": este botón tiene una segunda acción
	if h := b.Hold; h != nil && h.Tipo != "" {
		fs := base * 0.04
		bw, bh := fs*7.2, fs*2.3
		bx, by := r.X+r.W-p-bw, r.Y+p
		c.RoundRectStroke(bx+0.5, by+0.5, bw-1, bh-1, bh*0.35, 1, 0xFF454A54)
		c.Text("MANTENER", semiFont, fs, semiStyle, gMuted, RectF{bx, by, bw, bh}, AlignCenter, AlignCenter)
	}
	// nombre y tipo abajo a la izquierda
	fs := base * 0.088
	ss := base * 0.058
	sub := kindOf(b)
	if b.Tipo == "hotkey" && b.Keys != "" && b.Nombre != "" {
		sub = "Atajo · " + b.Keys
	}
	y := r.Y + r.H - p
	if sub != "" && flashText(id) == "" && confirmText(id) == "" {
		y -= ss * 1.4
		c.Text(sub, textFont, ss, FontStyleRegular, gMuted, RectF{r.X + p, y, r.W - 2*p, ss * 1.4}, AlignNear, AlignCenter)
		y -= ss * 0.25
	}
	c.TextWrap(caption, semiFont, fs, semiStyle, gText, RectF{r.X + p, y - fs*2.6, r.W - 2*p, fs * 2.6}, AlignNear, AlignFar)
}
