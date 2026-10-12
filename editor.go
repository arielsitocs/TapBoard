//go:build windows

package main

// Editor de botón (estilo Stream Deck): a la izquierda la vista previa, el
// nombre, el color y el ícono; a la derecha las categorías de acciones.

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

var (
	pickerOpen   bool
	pickerAnim   float32
	pickerBP     int
	pickerSlot   int
	pickerScroll float32

	edDraft       BotonP2
	edHoldMode    bool    // editando la acción de "mantener presionado"
	edMainDraft   BotonP2 // el botón (toque) mientras se edita la otra acción
	edHoldDraft   BotonP2 // la acción al mantener (Tipo "" = ninguna)
	edCat         int
	edIconMode    bool
	edNameEdited  bool
	edQuery       string
	edURLBuf      string
	edTextBuf     string
	edContent     float32
	edFixedHits   []sHit
	edScrollHits  []sHit
	edPressFixed  = -1
	edPressScroll = -1
	edDownY       int32
	edDownScroll  float32
	edScrolling   bool

	// teclado en pantalla
	kbdTarget  *string
	kbdShift   bool
	kbdSymbols bool
	kbdMulti   bool // permite Enter (texto)
	kbdHits    []sHit
	kbdPressed = -1
)

var edCats = []string{"Apps", "Sistema", "Multimedia", "Atajo", "Web", "Texto", "Archivo", "Panel"}

var swatches = []string{"", "#2E3B55", "#1F5B5D", "#2C4A3E", "#4A3466", "#5A2E4F", "#6E2A33", "#6D5328", "#3A3F47", "#1C1D20"}

func normSearch(s string) string {
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u")
	return strings.ToLower(r.Replace(s))
}

func matchesQuery(name string) bool {
	q := strings.TrimSpace(normSearch(edQuery))
	if q == "" {
		return true
	}
	n := normSearch(name)
	for _, w := range strings.Fields(q) {
		if !strings.Contains(n, w) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- abrir / cerrar

func openPicker(bp, slot int) {
	pickerBP, pickerSlot = bp, slot
	edDraft = pageSlots(bp)[slot]
	edHoldMode = false
	edHoldDraft = BotonP2{}
	if edDraft.Hold != nil {
		edHoldDraft = *edDraft.Hold
	}
	edDraft.Hold = nil
	loadDraftUI()
	pickerOpen = true
	startAnim()
}

// loadDraftUI prepara categoría, buffers y teclado para el borrador actual.
func loadDraftUI() {
	edNameEdited = edDraft.Nombre != "" && edDraft.Tipo != "app"
	edIconMode = false
	edQuery = ""
	edURLBuf, edTextBuf = "", ""
	if edDraft.Tipo == "url" {
		edURLBuf = edDraft.Ruta
	}
	if edDraft.Tipo == "text" {
		edTextBuf = edDraft.Text
	}
	kbdTarget = nil
	pickerScroll = 0
	switch edDraft.Tipo {
	case "app":
		edCat = 0
	case "media":
		edCat = 2
	case "hotkey":
		edCat = 3
	case "url":
		edCat = 4
	case "text":
		edCat = 5
	case "file":
		edCat = 6
	case "page":
		edCat = 7
	default:
		edCat = 1
	}
}

// setHoldMode cambia entre editar la acción del toque y la de mantener.
func setHoldMode(on bool) {
	if on == edHoldMode {
		return
	}
	stopRecording()
	if on {
		edMainDraft = edDraft
		edDraft = edHoldDraft
		if edDraft.Tipo == "" {
			edDraft = BotonP2{Tipo: "action", Accion: "empty"}
		}
	} else {
		edHoldDraft = edDraft
		edDraft = edMainDraft
	}
	edHoldMode = on
	loadDraftUI()
	invalidate(hwnd)
}

func holdIsSet(b BotonP2) bool {
	return b.Tipo != "" && !(b.Tipo == "action" && b.Accion == "empty")
}

func closePicker() {
	stopRecording()
	kbdTarget = nil
	pickerOpen = false
	startAnim()
}

func saveEditor() {
	setHoldMode(false)
	b := edDraft
	b.Hold = nil
	if holdIsSet(edHoldDraft) {
		h := edHoldDraft
		h.Hold = nil
		h.Color, h.Icon = "", ""
		b.Hold = &h
	}
	pageSlots(pickerBP)[pickerSlot] = b
	saveConfig(cfg)
	closePicker()
	toast("Botón guardado")
	invalidate(hwnd)
}

// setDraft cambia la acción manteniendo color, ícono y nombre propio.
func setDraft(b BotonP2) {
	b.Color = edDraft.Color
	b.Icon = edDraft.Icon
	if edNameEdited {
		b.Nombre = edDraft.Nombre
	}
	edDraft = b
	invalidate(hwnd)
}

func pickerTitle() string {
	p := 1
	if pickerBP >= 0 {
		p = panelPageOfBP(pickerBP) + 1
	}
	return "Botón " + itoa(pickerSlot+1) + " · Página " + itoa(p)
}

// ---------------------------------------------------------------- teclado

var (
	kbdRowsABC = []string{"qwertyuiop", "asdfghjklñ", "zxcvbnm"}
	kbdRowsSym = []string{"1234567890", "@#$%&-_+()", ".,:;/?!'\"="}
)

func kbdHeight() float32 {
	if kbdTarget == nil {
		return 0
	}
	return 4*40*panelSetScale() + 5*6*panelSetScale()
}

func kbdType(k string) {
	if kbdTarget == nil {
		return
	}
	t := kbdTarget
	switch k {
	case "⌫":
		if r := []rune(*t); len(r) > 0 {
			*t = string(r[:len(r)-1])
		}
	case "espacio":
		*t += " "
	case "↵":
		*t += "\n"
	case "Listo":
		kbdTarget = nil
	case "⇧":
		kbdShift = !kbdShift
	case "123", "abc":
		kbdSymbols = !kbdSymbols
	default:
		if kbdShift {
			k = strings.ToUpper(k)
			kbdShift = false
		}
		*t += k
	}
	if t == &edDraft.Nombre {
		edNameEdited = true
	}
	if t == &edQuery {
		pickerScroll = 0
	}
	if t == &edURLBuf {
		setDraft(BotonP2{Tipo: "url", Ruta: edURLBuf})
	}
	if t == &edTextBuf {
		setDraft(BotonP2{Tipo: "text", Text: edTextBuf})
	}
	invalidate(hwnd)
}

func paintKeyboard(c *Canvas, oy float32) {
	kbdHits = nil
	if kbdTarget == nil {
		return
	}
	s := panelSetScale()
	kh := kbdHeight()
	top := oy + panelH - kh
	c.RoundRect(0, top, panelW, kh+20*s, 16*s, 0xFF1C1D20)
	gp := 6 * s
	keyH := 40 * s
	kw := min32((panelW-gp*11)/10, 90*s)
	key := func(r RectF, label, typ string, col uint32) {
		i := len(kbdHits)
		kbdHits = append(kbdHits, sHit{r, func() { kbdType(typ) }})
		if kbdPressed == i {
			col = lighten(col, 0.2)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, 8*s, col)
		switch label {
		case "⌫":
			c.Text("\uE750", iconFont, 18*s, FontStyleRegular, sColText, r, AlignCenter, AlignCenter)
		case "⇧":
			c.Text("Mayús", semiFont, 13*s, semiStyle, sColText, r, AlignCenter, AlignCenter)
		case "↵":
			c.Text("Enter", semiFont, 13*s, semiStyle, sColText, r, AlignCenter, AlignCenter)
		default:
			c.Text(label, semiFont, 17*s, semiStyle, sColText, r, AlignCenter, AlignCenter)
		}
	}
	rows := kbdRowsABC
	if kbdSymbols {
		rows = kbdRowsSym
	}
	y := top + gp
	for ri, row := range rows {
		keys := []rune(row)
		n := len(keys)
		total := float32(n)*kw + float32(n-1)*gp
		if ri == 2 {
			total += (kw*1.4 + gp) * 2
		}
		x := (panelW - total) / 2
		if ri == 2 {
			col := uint32(0xFF4A4E55)
			if kbdShift {
				col = sColAccent
			}
			key(RectF{x, y, kw * 1.4, keyH}, "⇧", "⇧", col)
			x += kw*1.4 + gp
		}
		for _, k := range keys {
			lbl := string(k)
			if kbdShift && !kbdSymbols {
				lbl = strings.ToUpper(lbl)
			}
			key(RectF{x, y, kw, keyH}, lbl, string(k), 0xFF34373C)
			x += kw + gp
		}
		if ri == 2 {
			key(RectF{x, y, kw * 1.4, keyH}, "⌫", "⌫", 0xFF4A4E55)
		}
		y += keyH + gp
	}
	sym := "123"
	if kbdSymbols {
		sym = "abc"
	}
	bw := kw * 1.6
	sw := kw * 4.6
	n := 3
	if kbdMulti {
		n = 4
	}
	total := bw*float32(n-1) + sw + gp*float32(n)
	x := (panelW - total) / 2
	key(RectF{x, y, bw, keyH}, sym, sym, 0xFF4A4E55)
	x += bw + gp
	key(RectF{x, y, sw, keyH}, "espacio", "espacio", 0xFF34373C)
	x += sw + gp
	if kbdMulti {
		key(RectF{x, y, bw, keyH}, "↵", "↵", 0xFF4A4E55)
		x += bw + gp
	}
	key(RectF{x, y, bw, keyH}, "Listo", "Listo", sColAccent)
}

func editText(t *string, multi bool) {
	kbdTarget, kbdMulti, kbdShift, kbdSymbols = t, multi, false, false
	invalidate(hwnd)
}

// ---------------------------------------------------------------- dibujo

func pickerGeom() (ox, cw, oy float32) {
	e := 1 - (1-pickerAnim)*(1-pickerAnim)*(1-pickerAnim)
	return 0, panelW, (1 - e) * panelH
}

func edLeftW() float32 { return min32(panelW*0.3, 270*panelSetScale()) }

func edContentTop() float32 { return 120 * panelSetScale() }

func paintPicker(c *Canvas) {
	if pickerAnim < 0.01 {
		return
	}
	_, _, oy := pickerGeom()
	s := panelSetScale()
	c.ResetTransform()
	c.RoundRect(0, oy, panelW, panelH+pad*2, pad*2*(1-pickerAnim), themeBG())
	c.Translate(0, oy)
	edFixedHits = nil
	fixed := func(r RectF, fn func()) bool {
		edFixedHits = append(edFixedHits, sHit{r, fn})
		return edPressFixed == len(edFixedHits)-1
	}
	btn := func(r RectF, label string, col uint32, fn func()) {
		if fixed(r, fn) {
			col = lighten(col, 0.15)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, r.H/2, col)
		c.Text(label, semiFont, 14*s, semiStyle, 0xFFFFFFFF, r, AlignCenter, AlignCenter)
	}

	// cabecera
	c.Text(pickerTitle(), semiFont, 21*s, semiStyle, sColText, RectF{20 * s, 14 * s, panelW * 0.5, 40 * s}, AlignNear, AlignCenter)
	btn(RectF{panelW - 16*s - 120*s, 16 * s, 120 * s, 36 * s}, "Guardar", sColAccent, saveEditor)
	btn(RectF{panelW - 16*s - 120*s - 10*s - 112*s, 16 * s, 112 * s, 36 * s}, "Cancelar", 0xFF34373C, closePicker)
	// Toque / Mantener
	{
		segW := 112 * s
		sx := panelW - 16*s - 120*s - 10*s - 112*s - 18*s - segW*2
		sr := RectF{sx, 16 * s, segW * 2, 36 * s}
		c.RoundRect(sr.X, sr.Y, sr.W, sr.H, sr.H/2, 0xFF26282C)
		for k, name := range []string{"Toque", "Mantener"} {
			on := k == 1
			r := RectF{sx + float32(k)*segW, sr.Y, segW, sr.H}
			col, tc := uint32(0x00000000), uint32(sColSub)
			if edHoldMode == on {
				col, tc = sColAccent, 0xFFFFFFFF
			}
			if fixed(r, func() { setHoldMode(on) }) {
				col = lighten(col|0xFF000000, 0.12)
			}
			if col != 0 {
				c.RoundRect(r.X+2*s, r.Y+2*s, r.W-4*s, r.H-4*s, (r.H-4*s)/2, col)
			}
			c.Text(name, semiFont, 13.5*s, semiStyle, tc, r, AlignCenter, AlignCenter)
		}
	}

	// ---- columna izquierda: vista previa, nombre, color, ícono
	lw := edLeftW()
	lx := 16 * s
	y := 66 * s
	if edHoldMode {
		paintHoldLeft(c, lx, y, lw, s, btn)
	} else {
		paintTapLeft(c, lx, y, lw, s, btn, fixed)
	}
	paintEditorRight(c, oy, s, lx, lw, fixed)
}

func paintHoldLeft(c *Canvas, lx, y, lw, s float32, btn func(RectF, string, uint32, func())) {
	img, label, col := visualOf(edMainDraft)
	pr := RectF{lx, y, lw, lw * 0.66}
	if sn, m := appCardSnap(pr, col, img); sn != nil {
		c.DrawSnapshot(sn, ri(pr.X)-m, ri(pr.Y)-m)
		padI := pr.H * 0.11
		c.TextWrap(label, semiFont, min32(pr.W, pr.H)*0.088, semiStyle, 0xF2FFFFFF,
			RectF{pr.X + padI, pr.Y + pr.H*0.56, pr.W - 2*padI, pr.H*0.44 - padI*0.8}, AlignNear, AlignFar)
	}
	y += pr.H + 16*s
	c.Text("Al mantener presionado:", textFont, 13*s, FontStyleRegular, sColSub, RectF{lx, y, lw, 20 * s}, AlignNear, AlignCenter)
	y += 24 * s
	hl := "Nada"
	if holdIsSet(edDraft) {
		_, hl, _ = visualOf(edDraft)
	}
	c.RoundRect(lx, y, lw, 44*s, 12*s, 0xFF26282C)
	c.Text(hl, semiFont, 15*s, semiStyle, sColText, RectF{lx + 14*s, y, lw - 28*s, 44 * s}, AlignNear, AlignCenter)
	y += 44*s + 12*s
	if holdIsSet(edDraft) {
		btn(RectF{lx, y, lw, 38 * s}, "Quitar", 0xFF34373C, func() {
			edDraft = BotonP2{Tipo: "action", Accion: "empty"}
			loadDraftUI()
			invalidate(hwnd)
		})
		y += 38*s + 12*s
	}
}

func paintTapLeft(c *Canvas, lx, y, lw, s float32, btn func(RectF, string, uint32, func()), fixed func(RectF, func()) bool) {
	img, label, col := visualOf(edDraft)
	pr := RectF{lx, y, lw, lw * 0.66}
	if sn, m := appCardSnap(pr, col, img); sn != nil {
		c.DrawSnapshot(sn, ri(pr.X)-m, ri(pr.Y)-m)
		padI := pr.H * 0.11
		c.TextWrap(label, semiFont, min32(pr.W, pr.H)*0.088, semiStyle, 0xF2FFFFFF,
			RectF{pr.X + padI, pr.Y + pr.H*0.56, pr.W - 2*padI, pr.H*0.44 - padI*0.8}, AlignNear, AlignFar)
	}
	y += pr.H + 12*s
	// nombre
	nf := RectF{lx, y, lw, 40 * s}
	ncol := uint32(0xFF26282C)
	if kbdTarget == &edDraft.Nombre {
		ncol = 0xFF2C2F34
	}
	if fixed(nf, func() {
		if !edNameEdited {
			_, l, _ := visualOf(edDraft)
			edDraft.Nombre = l
			edNameEdited = true
		}
		editText(&edDraft.Nombre, false)
	}) {
		ncol = lighten(ncol, 0.1)
	}
	c.RoundRect(nf.X, nf.Y, nf.W, nf.H, 12*s, ncol)
	nameTxt := label
	if kbdTarget == &edDraft.Nombre {
		nameTxt = edDraft.Nombre + "|"
	}
	c.Text(nameTxt, textFont, 14.5*s, FontStyleRegular, sColText, RectF{nf.X + 14*s, nf.Y, nf.W - 28*s, nf.H}, AlignNear, AlignCenter)
	y += nf.H + 12*s
	// colores
	sz := (lw - 4*8*s) / 5
	for i, hx := range swatches {
		hx := hx
		x := lx + float32(i%5)*(sz+8*s)
		yy := y + float32(i/5)*(sz+8*s)
		r := RectF{x, yy, sz, sz}
		p := fixed(r, func() { edDraft.Color = hx; invalidate(hwnd) })
		cc := uint32(0xFF3A3D42)
		if hx != "" {
			cc, _ = parseHexColor(hx)
			cc = lighten(cc, 0.08)
		}
		if p {
			cc = lighten(cc, 0.15)
		}
		c.Circle(x+sz/2, yy+sz/2, sz/2, cc)
		if hx == "" {
			c.Text("Auto", semiFont, 10*s, semiStyle, sColText, r, AlignCenter, AlignCenter)
		}
		if hx == edDraft.Color {
			c.Arc(x+sz/2, yy+sz/2, sz/2-2*s, 0, 360, 3*s, 0xFFFFFFFF)
		}
	}
	y += 2*(sz+8*s) + 4*s
	// ícono
	half := (lw - 8*s) / 2
	ic := uint32(0xFF34373C)
	if edIconMode {
		ic = sColAccent
	}
	btn(RectF{lx, y, half, 38 * s}, "Ícono", ic, func() { edIconMode = !edIconMode; pickerScroll = 0; kbdTarget = nil })
	btn(RectF{lx + half + 8*s, y, half, 38 * s}, "Auto", 0xFF34373C, func() { edDraft.Icon = ""; invalidate(hwnd) })
}

func paintEditorRight(c *Canvas, oy, s, lx, lw float32, fixed func(RectF, func()) bool) {
	// ---- derecha: categorías
	rx := lx + lw + 18*s
	rw := panelW - rx - 16*s
	cy := 66 * s
	{
		gap := 6 * s
		var total float32
		for _, cName := range edCats {
			total += float32(len([]rune(cName)))*8.2*s + 22*s
		}
		k := float32(1)
		if total+gap*float32(len(edCats)-1) > rw {
			k = (rw - gap*float32(len(edCats)-1)) / total
		}
		x := rx
		for i, cName := range edCats {
			i := i
			w := (float32(len([]rune(cName)))*8.2*s + 22*s) * k
			r := RectF{x, cy, w, 36 * s}
			colc, tc := uint32(0xFF2A2C31), uint32(sColText)
			if i == edCat && !edIconMode {
				colc, tc = sColAccent, 0xFFFFFFFF
			}
			if fixed(r, func() { edCat, edIconMode, pickerScroll = i, false, 0; kbdTarget = nil; stopRecording() }) {
				colc = lighten(colc, 0.15)
			}
			c.RoundRect(r.X, r.Y, r.W, r.H, r.H/2, colc)
			c.Text(cName, semiFont, 12.5*s, semiStyle, tc, r, AlignCenter, AlignCenter)
			x += w + gap
		}
	}

	// ---- contenido (se desliza)
	top := edContentTop()
	visH := panelH - top - kbdHeight()
	c.ClipRoundRect(rx-4, top, rw+8, visH, 0)
	c.Translate(0, oy-pickerScroll)
	edScrollHits = nil
	L := &edLayout{c: c, s: s, x: rx, w: rw, y: top}
	if edIconMode {
		edIconGallery(L)
	} else {
		switch edCat {
		case 0:
			edApps(L)
		case 1:
			edSystem(L)
		case 2:
			edMedia(L)
		case 3:
			edHotkey(L)
		case 4:
			edWeb(L)
		case 5:
			edTextCat(L)
		case 6:
			edFile(L)
		case 7:
			edPanel(L)
		}
	}
	edContent = L.y - top + 16*s
	c.ResetClip()
	c.ResetTransform()
	if edContent > visH {
		bh := visH * visH / edContent
		by := pickerScroll / (edContent - visH) * (visH - bh)
		c.RoundRect(panelW-6*s, oy+top+by, 3*s, bh, 2*s, 0x50FFFFFF)
	}
	paintKeyboard(c, oy)
}

// ---------------------------------------------------------------- contenido

type edLayout struct {
	c       *Canvas
	s, x, w float32
	y       float32
}

func (L *edLayout) hit(r RectF, fn func()) bool {
	edScrollHits = append(edScrollHits, sHit{r, fn})
	return edPressScroll == len(edScrollHits)-1
}

func (L *edLayout) visible(y, h float32) bool {
	return y+h >= pickerScroll-60 && y <= pickerScroll+panelH+60
}

// grid dibuja una cuadrícula de opciones con ícono y nombre.
func (L *edLayout) grid(n int, item func(i int, vis bool) (*Image, string, uint32, bool, func())) {
	s := L.s
	cols := int(L.w / (112 * s))
	if cols < 3 {
		cols = 3
	}
	gap := 8 * s
	cw := (L.w - gap*float32(cols-1)) / float32(cols)
	ch := 96 * s
	for i := 0; i < n; i++ {
		x := L.x + float32(i%cols)*(cw+gap)
		y := L.y + float32(i/cols)*(ch+gap)
		img, label, col, sel, fn := item(i, L.visible(y, ch))
		r := RectF{x, y, cw, ch}
		p := L.hit(r, fn)
		if !L.visible(y, ch) {
			continue
		}
		col = themeTile(col)
		if p {
			col = lighten(col, 0.15)
		}
		L.c.RoundRect(r.X, r.Y, r.W, r.H, 14*s, col)
		if sel {
			L.c.RoundRectStroke(r.X+1, r.Y+1, r.W-2, r.H-2, 14*s, 3*s, sColAccent)
		}
		is := 40 * s
		if img != nil {
			L.c.DrawImage(img, r.X+(r.W-is)/2, r.Y+12*s, is, is)
		}
		L.c.TextWrap(label, textFont, 12*s, FontStyleRegular, 0xEEFFFFFF, RectF{r.X + 6*s, r.Y + 58*s, r.W - 12*s, 36 * s}, AlignCenter, AlignNear)
	}
	rows := (n + cols - 1) / cols
	L.y += float32(rows)*(ch+gap) + 6*s
}

// field: campo de texto que abre el teclado en pantalla.
func (L *edLayout) field(t *string, placeholder string, multi bool) {
	s := L.s
	h := 46 * s
	if multi {
		h = 96 * s
	}
	r := RectF{L.x, L.y, L.w, h}
	col := uint32(0xFF26282C)
	if kbdTarget == t {
		col = 0xFF2C2F34
	}
	if L.hit(r, func() { editText(t, multi) }) {
		col = lighten(col, 0.1)
	}
	L.c.RoundRect(r.X, r.Y, r.W, r.H, 14*s, col)
	if kbdTarget == t {
		L.c.RoundRect(r.X+10*s, r.Y+r.H-3*s, r.W-20*s, 2*s, 1*s, sColAccent)
	}
	txt, tc := *t, uint32(sColText)
	if txt == "" {
		txt, tc = placeholder, sColSub
	}
	if kbdTarget == t {
		txt = *t + "|"
		tc = sColText
	}
	if multi {
		L.c.TextWrap(txt, textFont, 15*s, FontStyleRegular, tc, RectF{r.X + 16*s, r.Y + 10*s, r.W - 32*s, r.H - 20*s}, AlignNear, AlignNear)
	} else {
		L.c.Text(txt, textFont, 15*s, FontStyleRegular, tc, RectF{r.X + 16*s, r.Y, r.W - 32*s, r.H}, AlignNear, AlignCenter)
	}
	L.y += h + 12*s
}

func (L *edLayout) button(label string, col uint32, fn func()) {
	s := L.s
	r := RectF{L.x, L.y, L.w, 46 * s}
	if L.hit(r, fn) {
		col = lighten(col, 0.15)
	}
	L.c.RoundRect(r.X, r.Y, r.W, r.H, r.H/2, col)
	L.c.Text(label, semiFont, 15*s, semiStyle, 0xFFFFFFFF, r, AlignCenter, AlignCenter)
	L.y += r.H + 10*s
}

func (L *edLayout) label(t string) {
	s := L.s
	L.c.Text(t, semiFont, 13*s, semiStyle, sColSub, RectF{L.x + 4*s, L.y, L.w, 24 * s}, AlignNear, AlignCenter)
	L.y += 28 * s
}

func edApps(L *edLayout) {
	L.field(&edQuery, "Buscar app…", false)
	appsMu.Lock()
	all, ready := appsList, appsReady
	appsMu.Unlock()
	var list []AppEntry
	for _, a := range all {
		if matchesQuery(a.Name) {
			list = append(list, a)
		}
	}
	if !ready {
		L.label("Buscando apps…")
		return
	}
	L.grid(len(list), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		a := list[i]
		var img *Image
		if vis { // el ícono se carga solo si está cerca de verse
			img, _ = appIcon(a.Ruta)
		}
		sel := edDraft.Tipo == "app" && edDraft.Ruta == a.Ruta
		return img, a.Name, 0xFF26282C, sel, func() {
			nb := BotonP2{Tipo: "app", Nombre: strings.TrimSpace(a.Name), Ruta: a.Ruta}
			if edNameEdited {
				nb.Nombre = edDraft.Nombre
			}
			nb.Color, nb.Icon = edDraft.Color, edDraft.Icon
			edDraft = nb
			invalidate(hwnd)
		}
	})
}

func edSystem(L *edLayout) {
	L.grid(len(actionDefs), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		a := actionDefs[i]
		sel := (edDraft.Tipo == "action" || edDraft.Tipo == "") && edDraft.Accion == a.key
		return icons[a.icon], a.label, a.col, sel, func() { setDraft(BotonP2{Tipo: "action", Accion: a.key}) }
	})
}

func edMedia(L *edLayout) {
	L.grid(len(mediaDefs), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		m := mediaDefs[i]
		sel := edDraft.Tipo == "media" && edDraft.Accion == m.key
		return icons[m.icon], m.label, m.col, sel, func() { setDraft(BotonP2{Tipo: "media", Accion: m.key}) }
	})
}

var hotkeyPresets = []struct{ label, keys, icon string }{
	{"Copiar", "Ctrl+C", "clipboard"},
	{"Pegar", "Ctrl+V", "clipboard"},
	{"Deshacer", "Ctrl+Z", "keyboard"},
	{"Cambiar ventana", "Alt+Tab", "desktop_computer"},
	{"Captura", "Win+Shift+S", "camera_with_flash"},
	{"Game Bar", "Win+G", "video_game"},
	{"Grabar clip", "Win+Alt+G", "film_frames"},
	{"Mute Discord", "Ctrl+Shift+M", "studio_microphone"},
	{"Nuevo escritorio", "Win+Ctrl+D", "desktop_computer"},
	{"Explorador", "Win+E", "card_index_dividers"},
	{"Pantalla completa", "F11", "television"},
	{"Cerrar pestaña", "Ctrl+W", "cross_mark"},
}

func edHotkey(L *edLayout) {
	s := L.s
	// combinación actual, grande
	r := RectF{L.x, L.y, L.w, 70 * s}
	L.c.RoundRect(r.X, r.Y, r.W, r.H, 16*s, 0xFF26282C)
	cur := edDraft.Keys
	if edDraft.Tipo != "hotkey" || cur == "" {
		cur = "Sin atajo"
	}
	if recBusy {
		cur = recordingPreview()
	}
	L.c.Text(cur, semiFont, 22*s, semiStyle, sColText, r, AlignCenter, AlignCenter)
	L.y += r.H + 12*s
	if recBusy {
		L.button("Cancelar grabación", 0xFF6E2A33, stopRecording)
	} else {
		L.button("Grabar atajo", sColAccent, func() {
			startRecording(func(combo string) { setDraft(BotonP2{Tipo: "hotkey", Keys: combo}) })
		})
	}
	L.label("Atajos comunes")
	L.grid(len(hotkeyPresets), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		p := hotkeyPresets[i]
		sel := edDraft.Tipo == "hotkey" && edDraft.Keys == p.keys
		return icons[p.icon], p.label + "\n" + p.keys, 0xFF2E3B55, sel, func() {
			setDraft(BotonP2{Tipo: "hotkey", Keys: p.keys})
			if !edNameEdited {
				edDraft.Nombre = p.label // nombre sugerido (se reemplaza si eliges otra cosa)
			}
		}
	})
}

var webPresets = []struct{ label, url string }{
	{"YouTube", "https://www.youtube.com"}, {"Google", "https://www.google.com"},
	{"WhatsApp Web", "https://web.whatsapp.com"}, {"Twitch", "https://www.twitch.tv"},
	{"Netflix", "https://www.netflix.com"}, {"Gmail", "https://mail.google.com"},
	{"ChatGPT", "https://chatgpt.com"}, {"Claude", "https://claude.ai"},
}

func edWeb(L *edLayout) {
	L.field(&edURLBuf, "Escribe una dirección web…", false)
	L.grid(len(webPresets), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		p := webPresets[i]
		sel := edDraft.Ruta == p.url
		return icons["globe_with_meridians"], p.label, 0xFF1F4E5A, sel, func() {
			edURLBuf = p.url
			setDraft(BotonP2{Tipo: "url", Ruta: p.url})
			if !edNameEdited {
				edDraft.Nombre = p.label // nombre sugerido (se reemplaza si eliges otra cosa)
			}
		}
	})
}

func edTextCat(L *edLayout) {
	L.field(&edTextBuf, "Texto a escribir…", true)
}

func edFile(L *edLayout) {
	if edDraft.Tipo == "file" && edDraft.Ruta != "" {
		L.label(edDraft.Ruta)
	}
	L.button("Elegir archivo…", sColAccent, func() {
		if p := fileDialogAny(false, "Elige un archivo"); p != "" {
			setDraft(BotonP2{Tipo: "file", Ruta: p})
		}
	})
	L.button("Elegir carpeta…", 0xFF34373C, func() {
		if p := pickFolder(); p != "" {
			setDraft(BotonP2{Tipo: "file", Ruta: p})
		}
	})
}

func edPanel(L *edLayout) {
	n := numPages()
	names := func(p int) string {
		switch p {
		case 0:
			return "Principal"
		case pageMonitor:
			return "Monitor"
		case pageMixer:
			return "Mezclador"
		}
		return "Botones"
	}
	L.grid(n, func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		sel := edDraft.Tipo == "page" && edDraft.Page == i+1
		return icons["right_arrow"], "Ir a página " + strconv.Itoa(i+1) + "\n" + names(i), 0xFF3D3550, sel,
			func() { setDraft(BotonP2{Tipo: "page", Page: i + 1}) }
	})
}

func edIconGallery(L *edLayout) {
	var names []string
	for k := range icons {
		if k == "tapboard_logo" || strings.HasPrefix(k, "fx_") {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	L.grid(len(names), func(i int, vis bool) (*Image, string, uint32, bool, func()) {
		k := names[i]
		return icons[k], "", 0xFF26282C, edDraft.Icon == k, func() { edDraft.Icon = k; invalidate(hwnd) }
	})
}

// ---------------------------------------------------------------- entrada

func clampPicker() {
	visH := panelH - edContentTop() - kbdHeight()
	maxS := edContent - visH
	if maxS < 0 {
		maxS = 0
	}
	if pickerScroll > maxS {
		pickerScroll = maxS
	}
	if pickerScroll < 0 {
		pickerScroll = 0
	}
}

func hitIn(list []sHit, fx, fy float32) int {
	for i := len(list) - 1; i >= 0; i-- {
		if inRect(list[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

func pickerDown(x, y int32) {
	_, _, oy := pickerGeom()
	fx, fy := float32(x), float32(y)-oy
	edDownY, edDownScroll, edScrolling = y, pickerScroll, false
	edPressFixed, edPressScroll, kbdPressed = -1, -1, -1
	if kbdTarget != nil && float32(y) >= panelH-kbdHeight() {
		kbdPressed = hitIn(kbdHits, float32(x), float32(y))
		if kbdPressed >= 0 {
			clickSound()
		}
		invalidate(hwnd)
		return
	}
	if fy < edContentTop() || fx < edLeftW()+24*panelSetScale() {
		edPressFixed = hitIn(edFixedHits, fx, fy)
	} else {
		edPressScroll = hitIn(edScrollHits, fx, fy+pickerScroll)
	}
	if edPressFixed >= 0 || edPressScroll >= 0 {
		clickSound()
	}
	invalidate(hwnd)
}

func pickerMove(x, y int32) {
	if kbdPressed >= 0 || edPressFixed >= 0 {
		return
	}
	dy := float32(y - edDownY)
	if !edScrolling && math.Abs(float64(dy)) > float64(panelH*0.025) {
		edScrolling = true
		edPressScroll = -1
	}
	if edScrolling {
		pickerScroll = edDownScroll - dy
		clampPicker()
		invalidate(hwnd)
	}
}

func pickerUp(x, y int32) {
	_, _, oy := pickerGeom()
	fx, fy := float32(x), float32(y)-oy
	switch {
	case kbdPressed >= 0:
		k := kbdPressed
		kbdPressed = -1
		if hitIn(kbdHits, float32(x), float32(y)) == k && k < len(kbdHits) {
			kbdHits[k].fn()
		}
	case edPressFixed >= 0:
		k := edPressFixed
		edPressFixed = -1
		if hitIn(edFixedHits, fx, fy) == k && k < len(edFixedHits) {
			edFixedHits[k].fn()
		}
	case edPressScroll >= 0 && !edScrolling:
		k := edPressScroll
		edPressScroll = -1
		if hitIn(edScrollHits, fx, fy+pickerScroll) == k && k < len(edScrollHits) {
			edScrollHits[k].fn()
		}
	}
	edScrolling = false
	invalidate(hwnd)
}
