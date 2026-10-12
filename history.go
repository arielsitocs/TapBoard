//go:build windows

package main

// Historial de notificaciones de WhatsApp: se abre deslizando hacia abajo.

import (
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	histMu   sync.Mutex
	waHist   []Notif // más nuevas primero
	histSeen = map[string]bool{}

	historyOpen   bool
	historyAnim   float32
	histScroll    float32
	histContent   float32
	histHits      []sHit
	histPressed   = -1
	histDownY     int32
	histDownScr   float32
	histScrolling bool
)

const histMax = 40

func addHistory(list []Notif) {
	histMu.Lock()
	defer histMu.Unlock()
	for _, n := range list {
		k := fmt.Sprint(n.Arrival, "|", n.Title, "|", n.Body)
		if histSeen[k] {
			continue
		}
		histSeen[k] = true
		waHist = append(waHist, n)
	}
	// Quitar duplicados: versiones viejas de la misma notificación y avisos
	// genéricos ("Nuevo mensaje") cuando está el mensaje real.
	waHist = dedupeNotifs(waHist)
	// ordenar de más nueva a más antigua
	for i := 1; i < len(waHist); i++ {
		for j := i; j > 0 && waHist[j].Arrival > waHist[j-1].Arrival; j-- {
			waHist[j], waHist[j-1] = waHist[j-1], waHist[j]
		}
	}
	if len(waHist) > histMax {
		waHist = waHist[:histMax]
	}
}

func getHistory() []Notif {
	histMu.Lock()
	defer histMu.Unlock()
	return append([]Notif(nil), waHist...)
}

func filetimeToTime(ft int64) time.Time {
	if ft <= 0 {
		return time.Time{}
	}
	return time.Unix(0, (ft-116444736000000000)*100)
}

func fmtWhen(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "ahora"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d.Minutes()))
	}
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return t.Format("15:04")
	}
	if now.Sub(t) < 48*time.Hour && now.Day() != t.Day() {
		return "ayer " + t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func openHistory() {
	histScroll = 0
	histPressed = -1
	historyOpen = true
	notifShown = false
	pageSound()
	startAnim()
}

func closeHistory() {
	historyOpen = false
	histPressed = -1
	startAnim()
}

func animHistory() bool {
	t := float32(0)
	if historyOpen {
		t = 1
	}
	if d := t - historyAnim; math.Abs(float64(d)) > 0.005 {
		historyAnim += d * 0.22
		return true
	}
	historyAnim = t
	return false
}

func paintHistory(c *Canvas) {
	if historyAnim < 0.01 {
		return
	}
	s := panelSetScale()
	e := 1 - (1-historyAnim)*(1-historyAnim)*(1-historyAnim)
	oy := -(1 - e) * panelH // baja desde arriba
	cw := min32(panelW, 860*s)
	ox := (panelW - cw) / 2
	c.ResetTransform()
	c.RoundRect(0, oy-pad*2, panelW, panelH+pad*2, pad*2*(1-historyAnim), 0xF0121314)
	c.Translate(ox, oy-histScroll)
	histHits = nil
	hit := func(r RectF, fn func()) bool {
		histHits = append(histHits, sHit{r, fn})
		return histPressed == len(histHits)-1
	}
	y := 20 * s
	is := 34 * s
	c.DrawImage(icons["speech_balloon"], 22*s, y, is, is)
	c.Text("WhatsApp", semiFont, 22*s, semiStyle, sColText, RectF{22*s + is + 10*s, y, cw * 0.5, is}, AlignNear, AlignCenter)
	{
		bh := 36 * s
		b1 := RectF{cw - 22*s - 104*s, y - 1*s, 104 * s, bh}
		b2 := RectF{b1.X - 10*s - 150*s, y - 1*s, 150 * s, bh}
		col := uint32(0xFF34373C)
		if hit(b1, closeHistory) {
			col = lighten(col, 0.15)
		}
		c.RoundRect(b1.X, b1.Y, b1.W, b1.H, bh/2, col)
		c.Text("Cerrar", semiFont, 14*s, semiStyle, sColText, b1, AlignCenter, AlignCenter)
		col = 0xFF1F6B47
		if hit(b2, func() { openWhatsApp(); closeHistory() }) {
			col = lighten(col, 0.15)
		}
		c.RoundRect(b2.X, b2.Y, b2.W, b2.H, bh/2, col)
		c.Text("Abrir WhatsApp", semiFont, 14*s, semiStyle, sColText, b2, AlignCenter, AlignCenter)
	}
	y += is + 18*s
	list := getHistory()
	if !cfg.WhatsApp {
		c.Text("Las notificaciones de WhatsApp están desactivadas en la configuración.", textFont, 14*s, FontStyleRegular, sColSub,
			RectF{24 * s, y, cw - 48*s, 30 * s}, AlignNear, AlignCenter)
		y += 40 * s
	} else if len(list) == 0 {
		c.Text("No hay notificaciones recientes.", textFont, 15*s, FontStyleRegular, sColSub,
			RectF{24 * s, y, cw - 48*s, 30 * s}, AlignNear, AlignCenter)
		y += 40 * s
	}
	ch := 70 * s
	for _, n := range list {
		r := RectF{16 * s, y, cw - 32*s, ch}
		col := uint32(0xFF1F2124)
		if hit(r, func() { openWhatsApp(); closeHistory() }) {
			col = lighten(col, 0.1)
		}
		if y+ch >= histScroll-20 && y <= histScroll+panelH+20 {
			c.RoundRect(r.X, r.Y, r.W, r.H, 14*s, col)
			c.RoundRect(r.X, r.Y+14*s, 4*s, r.H-28*s, 2*s, 0xFF25D366)
			when := fmtWhen(filetimeToTime(n.Arrival))
			c.Text(n.Title, semiFont, 15*s, semiStyle, sColText, RectF{r.X + 20*s, r.Y + 10*s, r.W - 160*s, 24 * s}, AlignNear, AlignCenter)
			c.Text(when, textFont, 12*s, FontStyleRegular, sColSub, RectF{r.X + r.W - 150*s, r.Y + 10*s, 134 * s, 24 * s}, AlignFar, AlignCenter)
			body := n.Body
			if !cfg.WhatsAppTexto || body == "" {
				body = "Nuevo mensaje"
			}
			c.Text(body, textFont, 13.5*s, FontStyleRegular, 0xFFC8CCD2, RectF{r.X + 20*s, r.Y + 36*s, r.W - 40*s, 24 * s}, AlignNear, AlignCenter)
		}
		y += ch + 10*s
	}
	y += 30 * s
	c.Text("Desliza hacia arriba para cerrar", textFont, 12*s, FontStyleRegular, 0xFF6F747C, RectF{0, y - 26*s, cw, 20 * s}, AlignCenter, AlignCenter)
	histContent = y
	c.ResetTransform()
}

func histHit(x, y int32) int {
	s := panelSetScale()
	cw := min32(panelW, 860*s)
	ox := (panelW - cw) / 2
	fx, fy := float32(x)-ox, float32(y)+histScroll
	for i := len(histHits) - 1; i >= 0; i-- {
		if inRect(histHits[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

func clampHist() {
	maxS := histContent - panelH
	if maxS < 0 {
		maxS = 0
	}
	if histScroll > maxS {
		histScroll = maxS
	}
	if histScroll < 0 {
		histScroll = 0
	}
}

func historyDown(x, y int32) {
	histDownY, histDownScr, histScrolling = y, histScroll, false
	histPressed = histHit(x, y)
	if histPressed >= 0 {
		clickSound()
	}
	invalidate(hwnd)
}

func historyMove(x, y int32) {
	dy := float32(y - histDownY)
	if !histScrolling && math.Abs(float64(dy)) > float64(panelH*0.025) {
		histScrolling = true
		histPressed = -1
	}
	if histScrolling {
		histScroll = histDownScr - dy
		clampHist()
		invalidate(hwnd)
	}
}

func historyUp(x, y int32) {
	dy := float32(y - histDownY)
	i := histHit(x, y)
	p := histPressed
	histPressed = -1
	// deslizar hacia arriba estando al tope = cerrar
	if histScrolling && dy < -panelH*0.15 && histDownScr >= histContent-panelH-1 {
		closeHistory()
	} else if histScrolling && dy < -panelH*0.15 && histContent <= panelH {
		closeHistory()
	} else if !histScrolling && i >= 0 && i == p && i < len(histHits) {
		histHits[i].fn()
	}
	histScrolling = false
	invalidate(hwnd)
}
