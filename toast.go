//go:build windows

package main

// Aviso corto abajo de la pantalla ("Abriendo Discord…").

import (
	"math"
	"time"
)

var (
	toastText  string
	toastUntil time.Time
	toastAnim  float32
)

const toastDur = 1600 * time.Millisecond

func toast(t string) {
	toastText = t
	toastUntil = time.Now().Add(toastDur)
	startAnim()
}

func animToast() bool {
	target := float32(0)
	if time.Now().Before(toastUntil) {
		target = 1
	}
	d := target - toastAnim
	if math.Abs(float64(d)) > 0.01 {
		toastAnim += d * 0.25
		return true
	}
	toastAnim = target
	return target == 1 // seguir animando hasta que se esconda
}

func paintToast(c *Canvas) {
	if toastAnim < 0.02 || toastText == "" {
		return
	}
	fs := panelH * 0.032
	if fs < 14 {
		fs = 14
	}
	w := float32(len([]rune(toastText)))*fs*0.56 + fs*2.4
	if w > panelW*0.9 {
		w = panelW * 0.9
	}
	h := fs * 2.3
	e := 1 - (1-toastAnim)*(1-toastAnim)
	x := (panelW - w) / 2
	y := panelH - h - (pad*1.2 + 10) - fs*0.4 + (1-e)*fs*1.2
	c.SetAlpha(e)
	c.RoundRect(x, y+3, w, h, h/2, 0x50000000)
	c.RoundRect(x, y, w, h, h/2, 0xF0303236)
	c.RoundRectStroke(x+0.5, y+0.5, w-1, h-1, h/2, 1, 0x22FFFFFF)
	c.Text(toastText, semiFont, fs, semiStyle, 0xFFF2F2F2, RectF{x + fs, y, w - fs*2, h}, AlignCenter, AlignCenter)
	c.SetAlpha(1)
}
