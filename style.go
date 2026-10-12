//go:build windows

package main

// Estilo visual: tarjetas "de vidrio", sombras, fondo con profundidad y
// colores tomados de la carátula del álbum.

import (
	"math"
	"time"
)

// glassCard dibuja una tarjeta con sombra suave, degradado, brillo superior y
// un borde fino claro.
// glassCard usa la caché: la tarjeta se dibuja una vez por tamaño y color.
func glassCard(c *Canvas, x, y, w, h, rad float32, col uint32) {
	m := 3 + int(h*0.06)
	iw, ih := ri(w), ri(h)
	key := "glass|" + itoa(iw) + "|" + itoa(ih) + "|" + itoa(ri(rad)) + "|" + hex32(col)
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		glassCardRaw(cc, float32(m), float32(m), float32(iw), float32(ih), rad, col)
	})
	if sn == nil {
		glassCardRaw(c, x, y, w, h, rad, col)
		return
	}
	c.DrawSnapshot(sn, ri(x)-m, ri(y)-m)
}

// flatCard: tarjeta plana (estilo minimalista), en caché.
func flatCard(c *Canvas, x, y, w, h, rad float32, col uint32) {
	iw, ih := ri(w), ri(h)
	if isGlass() {
		rad *= 0.62
	}
	key := "flat|" + itoa(iw) + "|" + itoa(ih) + "|" + itoa(ri(rad)) + "|" + hex32(col) + "|" + boolS(isGlass())
	m := 2 + int(float32(ih)*0.04)
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		if isGlass() {
			frost(cc, float32(m), float32(m), float32(iw), float32(ih), rad)
			return
		}
		cc.RoundRect(float32(m), float32(m), float32(iw), float32(ih), rad, col)
	})
	if sn != nil {
		c.DrawSnapshot(sn, ri(x)-m, ri(y)-m)
	}
}

func glassCardRaw(c *Canvas, x, y, w, h, rad float32, col uint32) {
	// sombra en dos capas (más suave)
	c.RoundRect(x+1, y+h*0.05, w-2, h, rad, 0x30000000)
	c.RoundRect(x, y+2, w, h, rad, 0x40000000)
	// cuerpo: degradado diagonal (luz desde arriba a la izquierda)
	c.RoundRectGradMode(x, y, w, h, rad, lighten(col, 0.16), darken(col, 0.18), 2)
	// brillo superior tipo vidrio
	c.RoundRectGrad(x, y, w, h*0.55, rad, 0x1CFFFFFF, 0x00FFFFFF)
	// borde: claro arriba, se pierde abajo
	c.RoundRectStrokeGrad(x+0.75, y+0.75, w-1.5, h-1.5, rad, 1.4, 0x40FFFFFF, 0x08FFFFFF)
}

// iconGlow: halo suave del color del botón detrás del ícono.
func iconGlow(c *Canvas, cx, cy, s float32, col uint32) {
	g := lighten(col, 0.45) & 0x00FFFFFF
	c.Ellipse(cx, cy, s*0.86, s*0.86, 0x07000000|g)
	c.Ellipse(cx, cy, s*0.70, s*0.70, 0x08000000|g)
	c.Ellipse(cx, cy, s*0.54, s*0.54, 0x09000000|g)
}

// paintBackground: fondo con un degradado sutil.
func paintBackground(c *Canvas) {
	bg := themeBG()
	w, h := ri(panelW), ri(panelH)
	sn := cachedRender("bg|"+itoa(w)+"|"+itoa(h)+"|"+hex32(bg)+"|"+cfg.Theme, w, h, func(cc *Canvas) {
		drawThemeBackground(cc, w, h)
	})
	if sn != nil {
		c.DrawSnapshot(sn, 0, 0)
	}
}

// iconDeco: halo + sombra del ícono, en caché.
func iconDeco(c *Canvas, cx, cy, s float32, col uint32) {
	is := ri(s * 1.8)
	key := "deco|" + itoa(is) + "|" + hex32(col)
	sn := cachedRender(key, is, is, func(cc *Canvas) {
		h := float32(is) / 2
		iconGlow(cc, h, h, s, col)
		iconShadow(cc, h, h, s)
	})
	if sn != nil {
		c.DrawSnapshot(sn, ri(cx)-is/2, ri(cy)-is/2)
	}
}

// iconShadow: sombra elíptica bajo un ícono 3D.
func iconShadow(c *Canvas, cx, cy, s float32) {
	c.Ellipse(cx, cy+s*0.47, s*0.34, s*0.07, 0x38000000)
}

// ---------------------------------------------------------------- carátula

var (
	mediaAmbient *Image // carátula muy reducida = desenfoque al estirarla
	mediaAccent  uint32 = colBlue
)

func buildAmbient(w, h int, pix []byte) {
	mediaAmbient, mediaAccent = nil, colBlue
	if pix == nil || w <= 0 || h <= 0 {
		return
	}
	const n = 7
	small := make([]byte, n*n*4)
	var best float64
	var br, bgc, bb float64
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var r, g, b, cnt int
			for sy := y * h / n; sy < (y+1)*h/n; sy++ {
				for sx := x * w / n; sx < (x+1)*w/n; sx++ {
					o := (sy*w + sx) * 4
					r += int(pix[o])
					g += int(pix[o+1])
					b += int(pix[o+2])
					cnt++
				}
			}
			if cnt == 0 {
				cnt = 1
			}
			fr, fg, fb := float64(r/cnt), float64(g/cnt), float64(b/cnt)
			// color más "vivo" = candidato a acento
			mx := math.Max(fr, math.Max(fg, fb))
			mn := math.Min(fr, math.Min(fg, fb))
			if sat := (mx - mn) * (0.4 + mx/255); sat > best {
				best, br, bgc, bb = sat, fr, fg, fb
			}
			i := (y*n + x) * 4
			small[i] = byte(fr * 0.55)
			small[i+1] = byte(fg * 0.55)
			small[i+2] = byte(fb * 0.55)
			small[i+3] = 255
		}
	}
	mediaAmbient = newImage(n, n, small)
	if best > 40 {
		// subir el brillo para que se vea bien sobre fondo oscuro
		mx := math.Max(br, math.Max(bgc, bb))
		k := 225 / math.Max(mx, 1)
		if k < 1 {
			k = 1
		}
		cl := func(v float64) uint32 { return uint32(math.Min(255, v*k)) }
		mediaAccent = 0xFF000000 | cl(br)<<16 | cl(bgc)<<8 | cl(bb)
	}
}

// drawAmbient pinta la carátula difuminada como fondo de la tarjeta.
func drawAmbient(c *Canvas, r RectF, rad float32) {
	if mediaAmbient == nil {
		return
	}
	w, h := ri(r.W), ri(r.H)
	key := "amb|" + itoa(mediaAmbient.id) + "|" + itoa(w) + "|" + itoa(h) + "|" + itoa(ri(rad))
	sn := cachedRender(key, w, h, func(cc *Canvas) {
		drawAmbientRaw(cc, RectF{0, 0, float32(w), float32(h)}, rad)
	})
	if sn != nil {
		c.DrawSnapshot(sn, ri(r.X), ri(r.Y))
	}
}

func drawAmbientRaw(c *Canvas, r RectF, rad float32) {
	c.ClipRoundRect(r.X, r.Y, r.W, r.H, rad)
	c.drawImageHQ(mediaAmbient, r.X-r.W*0.1, r.Y-r.H*0.8, r.W*1.2, r.H*2.6)
	// oscurecer hacia la derecha para que el texto se lea
	c.RoundRectGradH(r.X, r.Y, r.W, r.H, 0, 0x30101114, 0xC0101114)
	c.ResetClip()
	c.RoundRectStroke(r.X+0.5, r.Y+0.5, r.W-1, r.H-1, rad, 1.2, 0x22FFFFFF)
}

// loadColor: verde → ámbar → rojo según la carga (0-100).
func loadColor(v float64) uint32 {
	switch {
	case v >= 85:
		return 0xFFEF5350
	case v >= 60:
		return 0xFFFFB74D
	}
	return 0xFF66BB6A
}

// ring dibuja un medidor circular.
func ring(c *Canvas, cx, cy, r, frac float32, col uint32) {
	w := r * 0.22
	c.Arc(cx, cy, r, 0, 360, w, 0x22FFFFFF)
	if frac > 0.004 {
		c.Arc(cx, cy, r, -90, 360*float32(math.Min(1, float64(frac))), w, col)
	}
}

// ---------------------------------------------------------------- rápido

var (
	bgDC, bgBmp, bgOld uintptr
	bgKey              string
)

// blitBackground copia el fondo (dibujado una vez) al búfer con GDI, que es
// muchísimo más rápido que pintarlo con GDI+ en cada cuadro.
func blitBackground(hdc uintptr, w, h int32) {
	key := itoa(int(w)) + "|" + itoa(int(h)) + "|" + hex32(themeBG()) + "|" + cfg.Theme
	if bgDC == 0 || key != bgKey {
		if bgDC != 0 {
			pSelectObject.Call(bgDC, bgOld)
			pDeleteObject.Call(bgBmp)
			pDeleteDC.Call(bgDC)
		}
		bgDC, _, _ = pCreateCompatibleDC.Call(hdc)
		bgBmp, _, _ = pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
		bgOld, _, _ = pSelectObject.Call(bgDC, bgBmp)
		c := newCanvas(bgDC)
		drawThemeBackground(c, int(w), int(h))
		c.Close()
		bgKey = key
	}
	pBitBlt.Call(backDC, 0, 0, uintptr(w), uintptr(h), bgDC, 0, 0, SRCCOPY)
}

func hasRipple(i int) bool {
	for _, r := range ripples {
		if r.tile == i {
			return true
		}
	}
	return false
}

// drawTileFast: tarjeta + halo + ícono en una sola imagen en caché; el texto va encima.
func drawTileFast(c *Canvas, i int, col uint32, img *Image, caption string) {
	r := rectOf(i)
	col = themeTile(col)
	base := min32(r.W, r.H)
	s := base * 0.42
	cyOff := r.H / 2
	if caption != "" {
		s = base * 0.38
		cyOff = r.H * 0.40
	}
	m := 3 + int(r.H*0.06)
	iw, ih := ri(r.W), ri(r.H)
	imgID := 0
	if img != nil {
		imgID = img.id
	}
	key := "tile|" + itoa(iw) + "|" + itoa(ih) + "|" + hex32(col) + "|" + itoa(imgID) + "|" + itoa(ri(s)) + "|" + itoa(ri(cyOff))
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		x, y := float32(m), float32(m)
		glassCardRaw(cc, x, y, float32(iw), float32(ih), radius(r), col)
		if img != nil {
			cx, cy := x+float32(iw)/2, y+cyOff
			iconGlow(cc, cx, cy, s, col)
			iconShadow(cc, cx, cy, s)
			cc.drawImageHQ(img, cx-s/2, cy-s/2, s, s)
		}
	})
	if sn == nil {
		return
	}
	c.DrawSnapshot(sn, ri(r.X)-m, ri(r.Y)-m)
	if caption != "" {
		cr := RectF{r.X + r.W*0.06, r.Y + r.H*0.64, r.W * 0.88, r.H * 0.28}
		c.TextWrap(caption, semiFont, base*0.095, semiStyle, 0xEEFFFFFF, cr, AlignCenter, AlignCenter)
	}
}

// volCardBG: tarjeta de volumen (+ carátula difuminada) en una sola imagen.
func volCardBG(c *Canvas, r RectF) {
	rad := minimalRadius(r)
	col := themeTile(0xFF202124)
	amb, thumb := 0, 0
	show := mediaShowing()
	if show && mediaAmbient != nil {
		amb = mediaAmbient.id
	}
	if mediaThumb != nil {
		thumb = mediaThumb.id
	}
	m := 3 + int(r.H*0.06)
	iw, ih := ri(r.W), ri(r.H)
	key := "volbg|" + itoa(iw) + "|" + itoa(ih) + "|" + hex32(col) + "|" + itoa(amb) + "|" + boolS(isGlass())
	if show {
		key += "|m|" + itoa(thumb) + "|" + boolS(media.End > 0) + "|" + boolS(aud.Muted)
	}
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		x, y := float32(m), float32(m)
		rr := RectF{x, y, float32(iw), float32(ih)}
		if isGlass() {
			frost(cc, x, y, float32(iw), float32(ih), rad)
		} else {
			cc.RoundRect(x, y, float32(iw), float32(ih), rad, col)
		}
		if amb != 0 {
			drawAmbientRaw(cc, rr, rad)
		}
		if show {
			mediaStatic(cc, rr)
			// franja del volumen con su ícono y la pista de la barra
			sb := volStripIn(rr)
			cc.RoundRect(sb.X, sb.Y, sb.W, sb.H, sb.H/2, 0x14FFFFFF)
			ic := "speaker_high_volume"
			if aud.Muted {
				ic = "muted_speaker"
			}
			is := sb.H * 0.62
			cc.drawImageHQ(icons[ic], sb.X+sb.H*0.32, sb.Y+(sb.H-is)/2, is, is)
			th := rr.H * 0.045
			x0, x1, ty := sb.X+sb.H*1.15, sb.X+sb.W-sb.H*1.9, sb.Y+sb.H/2
			cc.RoundRect(x0, ty-th/2, x1-x0, th, th/2, colTrack)
		}
	})
	if sn != nil {
		c.DrawSnapshot(sn, ri(r.X)-m, ri(r.Y)-m)
	}
}

func boolS(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ---------------------------------------------------------------- estilos de botón

var bounces = map[int]time.Time{}

const bounceDur = 260 * time.Millisecond

// bounceScale: pequeño "rebote" al soltar un botón.
func bounceScale(i int) float32 {
	t0, ok := bounces[i]
	if !ok {
		return 0
	}
	t := float64(time.Since(t0)) / float64(bounceDur)
	if t >= 1 {
		return 0
	}
	return float32(0.035 * math.Sin(math.Pi*t) * (1 - t*0.4))
}

func bouncing() bool {
	busy := false
	for k, t0 := range bounces {
		if time.Since(t0) >= bounceDur {
			delete(bounces, k)
		} else {
			busy = true
		}
	}
	return busy
}

func animating(i int) bool {
	_, b := bounces[i]
	return pressAnim[i] > 0.01 || b
}

// drawCached dibuja la imagen en caché del botón: tamaño real si está quieto,
// escalada (rápido) mientras se anima.
func drawCached(c *Canvas, i int, sn *Snapshot, m int) RectF {
	r0 := rectOf(i)
	if !animating(i) {
		c.DrawSnapshot(sn, ri(r0.X)-m, ri(r0.Y)-m)
		return r0
	}
	vr := visualRect(i)
	k := vr.W / float32(ri(r0.W))
	c.DrawSnapshotRect(sn, vr.X-float32(m)*k, vr.Y-float32(m)*k, float32(sn.w)*k, float32(sn.h)*k)
	return vr
}

// Estilo 1 (página principal): minimalista, un solo color, ícono centrado.
func minimalRadius(r RectF) float32 {
	if isGlass() {
		return min32(r.W, r.H) * 0.15 // esquinas más sutiles en el vidrio
	}
	return min32(r.W, r.H) * 0.26
}

// cardRadius: radio de las tarjetas de las páginas de botones.
func cardRadius(base float32) float32 {
	if isGlass() {
		return base * 0.12
	}
	return base * 0.2
}

func drawMinimalTile(c *Canvas, i int, col uint32, img *Image, caption string) {
	r := rectOf(i)
	col = themeTile(col)
	base := min32(r.W, r.H)
	s := base * 0.42
	cyOff := r.H / 2
	if caption != "" {
		s = base * 0.36
		cyOff = r.H * 0.40
	}
	glass := isGlass()
	if glass {
		s *= 0.8 // íconos más livianos en el vidrio
	}
	if img != nil && img.mono {
		s *= 0.78
	}
	m := 2
	iw, ih := ri(r.W), ri(r.H)
	imgID := 0
	if img != nil {
		imgID = img.id
	}
	rad := minimalRadius(r)
	if glass {
		m = 2 + int(r.H*0.04)
	}
	key := "min|" + itoa(iw) + "|" + itoa(ih) + "|" + hex32(col) + "|" + itoa(imgID) + "|" + itoa(ri(s)) + "|" + itoa(ri(cyOff)) + "|" + boolS(glass)
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		x, y := float32(m), float32(m)
		if glass {
			frost(cc, x, y, float32(iw), float32(ih), rad)
		} else {
			cc.RoundRect(x, y, float32(iw), float32(ih), rad, col)
		}
		if img != nil {
			cx, cy := x+float32(iw)/2, y+cyOff
			cc.drawImageHQ(img, cx-s/2, cy-s/2, s, s)
		}
	})
	if sn == nil {
		return
	}
	vr := drawCached(c, i, sn, m)
	// al presionar se ilumina con un acento del mismo color
	if pa := pressAnim[i]; pa > 0.01 {
		if glass {
			c.RoundRect(vr.X, vr.Y, vr.W, vr.H, minimalRadius(vr), uint32(40*pa)<<24|0xFFFFFF)
		} else {
			c.RoundRect(vr.X, vr.Y, vr.W, vr.H, minimalRadius(vr), uint32(70*pa)<<24|lighten(col, 0.6)&0xFFFFFF)
		}
	}
	if caption != "" {
		cr := RectF{vr.X + vr.W*0.06, vr.Y + vr.H*0.64, vr.W * 0.88, vr.H * 0.28}
		c.TextWrap(caption, semiFont, base*0.09, semiStyle, 0xE6FFFFFF, cr, AlignCenter, AlignCenter)
	}
}

// Estilo 3 (páginas de botones): tarjeta con el color de la app, ícono arriba
// a la izquierda y el nombre abajo a la izquierda.
func cardColor(col uint32) uint32 {
	c := themeTile(col)
	// asegurar un mínimo de brillo para que el degradado luzca
	r, g, b := (c>>16)&0xFF, (c>>8)&0xFF, c&0xFF
	if mx := max3(r, g, b); mx < 110 {
		c = lighten(c, float32(110-mx)/255*1.2)
	}
	return c
}

func max3(a, b, c uint32) uint32 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

func drawAppCard(c *Canvas, i int, col uint32, img *Image, caption string) {
	r := rectOf(i)
	sn, m := appCardSnap(r, col, img)
	if sn == nil {
		return
	}
	base := min32(r.W, r.H)
	rad := cardRadius(base)
	padI := r.H * 0.11
	vr := drawCached(c, i, sn, m)
	if pa := pressAnim[i]; pa > 0.01 {
		c.RoundRect(vr.X, vr.Y, vr.W, vr.H, rad*vr.W/r.W, uint32(60*pa)<<24|0xFFFFFF)
	}
	if caption != "" {
		k := vr.W / r.W
		cr := RectF{vr.X + padI*k, vr.Y + vr.H*0.56, vr.W - 2*padI*k, vr.H*0.44 - padI*k*0.8}
		fs := base * 0.088
		if isGlass() {
			fs = base * 0.078
		}
		c.TextWrap(caption, semiFont, fs, semiStyle, 0xF2FFFFFF, cr, AlignNear, AlignFar)
	}
}

func appCardSnap(r RectF, col uint32, img *Image) (*Snapshot, int) {
	cc0 := cardColor(col)
	base := min32(r.W, r.H)
	rad := cardRadius(base)
	padI := r.H * 0.11
	is := base * 0.34
	if img != nil && img.mono {
		is = base * 0.27
	}
	glass := isGlass()
	if glass {
		is = base * 0.24
	}
	m := 3 + int(r.H*0.05)
	iw, ih := ri(r.W), ri(r.H)
	imgID := 0
	if img != nil {
		imgID = img.id
	}
	key := "card|" + itoa(iw) + "|" + itoa(ih) + "|" + hex32(cc0) + "|" + itoa(imgID) + "|" + boolS(glass)
	sn := cachedRender(key, iw+2*m, ih+2*m, func(cc *Canvas) {
		x, y := float32(m), float32(m)
		w, h := float32(iw), float32(ih)
		if glass {
			frost(cc, x, y, w, h, rad)
			if img != nil {
				cc.drawImageHQ(img, x+padI, y+padI, is, is)
			} else {
				cc.Text("+", textFont, h*0.3, FontStyleRegular, 0x55FFFFFF, RectF{x, y, w, h * 0.8}, AlignCenter, AlignCenter)
			}
			return
		}
		cc.RoundRect(x, y+3, w, h, rad, 0x38000000) // sombra suave
		cc.RoundRectGradMode(x, y, w, h, rad, lighten(cc0, 0.22), darken(cc0, 0.22), 2)
		// brillo suave arriba
		cc.RoundRectGrad(x, y, w, h*0.6, rad, 0x16FFFFFF, 0x00FFFFFF)
		if img != nil {
			if !img.mono {
				cc.Ellipse(x+padI+is/2, y+padI+is*1.02, is*0.36, is*0.07, 0x30000000)
			}
			cc.drawImageHQ(img, x+padI, y+padI, is, is)
		} else {
			cc.Text("+", textFont, h*0.3, FontStyleRegular, 0x55FFFFFF, RectF{x, y, w, h * 0.8}, AlignCenter, AlignCenter)
		}
	})
	return sn, m
}
