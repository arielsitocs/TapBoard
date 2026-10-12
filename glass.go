//go:build windows

package main

// Tema "Vidrio": fondo con luces de color difuminadas y tarjetas translúcidas.

import (
	"math"
)

type glow struct {
	x, y, r float64 // posición y radio relativos al ancho/alto del panel
	col     uint32
}

var glassGlows = []glow{
	{0.19, 0.28, 0.30, 0xFF2347D6}, // azul, arriba a la izquierda
	{0.80, 0.12, 0.30, 0xFF7A2FD0}, // morado, arriba a la derecha
	{0.58, 0.88, 0.30, 0xFF0F8F88}, // verde azulado, abajo
	{0.06, 0.92, 0.22, 0xFF7A2350}, // magenta, abajo a la izquierda
}

var (
	wallKey string
	wallImg *Image
)

// glassWallpaper genera (una vez por tamaño) el fondo del tema Vidrio.
func glassWallpaper(w, h int) *Image {
	key := itoa(w) + "x" + itoa(h)
	if wallImg != nil && wallKey == key {
		return wallImg
	}
	base := themeBG()
	br, bg, bb := float64((base>>16)&0xFF), float64((base>>8)&0xFF), float64(base&0xFF)
	pix := make([]byte, w*h*4)
	fw, fh := float64(w), float64(h)
	seed := uint32(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := br, bg, bb
			for _, gl := range glassGlows {
				dx := (float64(x) - gl.x*fw) / fw
				dy := (float64(y) - gl.y*fh) / fw
				s := gl.r * 0.62
				a := 0.50 * math.Exp(-(dx*dx+dy*dy)/(2*s*s))
				r += (float64((gl.col>>16)&0xFF) - r) * a
				g += (float64((gl.col>>8)&0xFF) - g) * a
				b += (float64(gl.col&0xFF) - b) * a
			}
			// un poco de ruido para que el degradado no se vea en franjas
			seed = seed*1664525 + 1013904223
			n := float64(int(seed>>24)%3-1) * 0.8
			i := (y*w + x) * 4
			pix[i] = clampByte(r + n)
			pix[i+1] = clampByte(g + n)
			pix[i+2] = clampByte(b + n)
			pix[i+3] = 255
		}
	}
	wallImg = newImage(w, h, pix)
	wallKey = key
	return wallImg
}

func clampByte(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

// drawThemeBackground pinta el fondo del panel según el tema.
func drawThemeBackground(c *Canvas, w, h int) {
	if isGlass() {
		c.Clear(themeBG())
		c.drawImageHQ(glassWallpaper(w, h), 0, 0, float32(w), float32(h))
		return
	}
	bg := themeBG()
	c.Clear(bg)
	c.RoundRectGrad(-1, -1, float32(w)+2, float32(h)+2, 0, lighten(bg, 0.05), darken(bg, 0.25))
}

// frost dibuja una tarjeta de vidrio: velo blanco translúcido, brillo diagonal
// y un borde fino que se ilumina arriba.
func frost(c *Canvas, x, y, w, h, rad float32) {
	c.RoundRect(x, y+h*0.03, w, h, rad, 0x22000000) // sombra muy suave
	c.RoundRect(x, y, w, h, rad, 0x17FFFFFF)
	c.RoundRectGradMode(x, y, w, h, rad, 0x16FFFFFF, 0x00FFFFFF, 2)
	c.RoundRectStrokeGrad(x+0.5, y+0.5, w-1, h-1, rad, 1, 0x38FFFFFF, 0x12FFFFFF)
}
