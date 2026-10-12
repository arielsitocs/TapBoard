//go:build windows

package main

// Temas de color y modo reposo.

import (
	"fmt"
	"math"
	"os"
	"time"
)

var themeKeys = []string{"dark", "oled", "graphite", "midnight"}
var themeLabels = []string{"Oscuro", "OLED", "Grafito", "Medianoche"}

func themeIndex() int {
	for i, k := range themeKeys {
		if k == cfg.Theme {
			return i
		}
	}
	return 0
}

func themeBG() uint32 {
	switch cfg.Theme {
	case "oled":
		return 0xFF000000
	case "graphite":
		return 0xFF1F2023
	case "midnight":
		return 0xFF0B1220
	}
	return colBG
}

func mix(a, b uint32, t float32) uint32 {
	ch := func(sh uint) uint32 {
		x := float32((a>>sh)&0xFF)*(1-t) + float32((b>>sh)&0xFF)*t
		return uint32(x) << sh
	}
	return 0xFF000000 | ch(16) | ch(8) | ch(0)
}

// themeTile ajusta el color de un botón o tarjeta según el tema.
func themeTile(col uint32) uint32 {
	col = vivid(col)
	switch cfg.Theme {
	case "oled":
		return darken(col, 0.35)
	case "graphite":
		return lighten(col, 0.06)
	case "midnight":
		return mix(col, 0xFF1C2E55, 0.3)
	}
	return col
}

// ---------------------------------------------------------------- reposo

var (
	idle      bool
	idleAnim  float32
	lastTouch = time.Now()
	swallow   bool // el toque que despierta no activa botones
)

var idleOptions = []int{0, 1, 3, 5, 10}

func idleIndex() int {
	for i, v := range idleOptions {
		if v == cfg.IdleMinutes {
			return i
		}
	}
	return 2
}

func checkIdle() {
	if idle || cfg.IdleMinutes <= 0 || settingsOpen || pickerOpen || historyOpen || activePointer != math.MinInt64 {
		return
	}
	limit := time.Duration(cfg.IdleMinutes) * time.Minute
	if demo && os.Getenv("PANEL_DEMO_IDLE") != "" {
		limit = 6 * time.Second
	}
	if time.Since(lastTouch) > limit {
		idle = true
		startAnim()
	}
}

func wake() {
	lastTouch = time.Now()
	if idle {
		idle = false
		swallow = true
		startAnim()
	}
}

func animIdle() bool {
	t := float32(0)
	if idle {
		t = 1
	}
	d := t - idleAnim
	if math.Abs(float64(d)) > 0.005 {
		k := float32(0.06) // apagado lento
		if t == 0 {
			k = 0.3 // despertar rápido
		}
		idleAnim += d * k
		return true
	}
	idleAnim = t
	return false
}

func paintIdle(c *Canvas) {
	if idleAnim < 0.01 {
		return
	}
	a := uint32(255 * math.Min(1, float64(idleAnim)))
	c.ResetTransform()
	c.RoundRect(-2, -2, panelW+4, panelH+4, 0, a<<24) // negro encima
	if idleAnim < 0.6 {
		return
	}
	ta := uint32(255 * math.Min(1, float64(idleAnim-0.6)/0.4))
	now := time.Now()
	// pequeño desplazamiento cada minuto para no marcar la pantalla
	dx := float32(math.Sin(float64(now.Minute()))) * panelW * 0.03
	dy := float32(math.Cos(float64(now.Minute()))) * panelH * 0.03
	col := func(c uint32) uint32 { return (c & 0x00FFFFFF) | (ta*((c>>24)&0xFF)/255)<<24 }
	clockS := panelH * 0.3
	y := panelH*0.16 + dy
	c.Text(now.Format("15:04"), semiFont, clockS, semiStyle, col(0xFFB8BCC4), RectF{dx, y, panelW, clockS * 1.3}, AlignCenter, AlignCenter)
	dias := []string{"Domingo", "Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado"}
	meses := []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	fs := panelH * 0.045
	y += clockS * 1.3
	c.Text(fmt.Sprintf("%s %d de %s", dias[now.Weekday()], now.Day(), meses[now.Month()-1]), textFont, fs, FontStyleRegular,
		col(0xFF7A7F88), RectF{dx, y, panelW, fs * 1.6}, AlignCenter, AlignCenter)
	y += fs * 2.6
	if mediaShowing() {
		art := panelH * 0.1
		title := sinEmojis(media.Title)
		if media.Artist != "" {
			title += " · " + sinEmojis(media.Artist)
		}
		w := panelW * 0.6
		x := (panelW-w)/2 + dx
		if mediaThumb != nil {
			c.ClipRoundRect(x, y, art, art, art*0.18)
			c.DrawImage(mediaThumb, x, y, art, art)
			c.ResetClip()
		}
		c.Text(title, textFont, fs*0.9, FontStyleRegular, col(0xFF8A8F98), RectF{x + art + art*0.25, y, w - art*1.25, art}, AlignNear, AlignCenter)
	}
	c.Text("Toca para volver", textFont, fs*0.6, FontStyleRegular, col(0xFF3E4248), RectF{0, panelH - fs*2, panelW, fs * 1.2}, AlignCenter, AlignCenter)
}

// vivid aumenta un poco la saturación de los colores de los botones.
func vivid(col uint32) uint32 {
	r := float32((col >> 16) & 0xFF)
	g := float32((col >> 8) & 0xFF)
	b := float32(col & 0xFF)
	l := (r + g + b) / 3
	k := float32(1.35)
	f := func(v float32) uint32 {
		x := l + (v-l)*k
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		return uint32(x)
	}
	return col&0xFF000000 | f(r)<<16 | f(g)<<8 | f(b)
}
