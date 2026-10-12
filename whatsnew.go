//go:build windows

package main

// "Novedades": después de actualizarse, muestra la lista de cambios de la
// versión nueva (tomada del Release de GitHub) en una tarjeta sobre el panel.

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	wnOpen    bool
	wnAnim    float32
	wnItems   []string
	wnVersion string
	wnCh      = make(chan []string, 1)
)

var reMdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// releaseNotes trae los puntos de la lista de cambios de una versión.
func releaseNotes(version string) []string {
	api := "https://api.github.com/repos/" + updRepo + "/releases/tags/v" + version
	if u := os.Getenv("TAPBOARD_NOTES_API"); u != "" {
		api = u // solo para pruebas
	}
	req, _ := http.NewRequest("GET", api, nil)
	req.Header.Set("User-Agent", "TapBoard/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var rel struct {
		Body string `json:"body"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return nil
	}
	var items []string
	for _, ln := range strings.Split(rel.Body, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "- ") && !strings.HasPrefix(ln, "* ") {
			continue
		}
		ln = strings.TrimSpace(ln[2:])
		ln = reMdLink.ReplaceAllString(ln, "$1")
		ln = strings.NewReplacer("**", "", "__", "", "`", "").Replace(ln)
		ln = sinEmojis(ln)
		if ln == "" {
			continue
		}
		items = append(items, ln)
		if len(items) == 8 {
			break
		}
	}
	return items
}

// maybeShowWhatsNew se llama al iniciar: si la app se acaba de actualizar,
// busca la lista de cambios y la muestra.
func maybeShowWhatsNew() {
	prev := cfg.SeenVersion
	if prev != appVersion {
		cfg.SeenVersion = appVersion
		saveConfig(cfg)
	}
	if !justUpdated() && (prev == "" || prev == appVersion) {
		return
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		items := releaseNotes(appVersion)
		if len(items) == 0 {
			items = []string{"Mejoras y correcciones"}
		}
		wnCh <- items
	}()
}

func receiveWhatsNew() {
	select {
	case items := <-wnCh:
		wnItems, wnVersion = items, appVersion
		wnOpen = true
		lastTouch = time.Now()
		startAnim()
	default:
	}
}

func animWhatsNew() bool {
	t := float32(0)
	if wnOpen {
		t = 1
	}
	d := t - wnAnim
	if d > 0.01 || d < -0.01 {
		wnAnim += d * 0.22
		return true
	}
	wnAnim = t
	return false
}

func closeWhatsNew() {
	wnOpen = false
	startAnim()
}

func paintWhatsNew(c *Canvas) {
	if wnAnim < 0.01 {
		return
	}
	e := 1 - (1-wnAnim)*(1-wnAnim)
	c.SetAlpha(e)
	defer c.SetAlpha(1)
	c.RoundRect(-2, -2, panelW+4, panelH+4, 0, 0xB0000000)

	s := panelH / 480
	w := min32(panelW*0.78, 620*s)
	rowH := 30 * s
	h := 96*s + float32(len(wnItems))*rowH + 44*s
	if h > panelH*0.92 {
		h = panelH * 0.92
	}
	x := (panelW - w) / 2
	y := (panelH-h)/2 + (1-e)*24*s
	c.RoundRect(x, y+6*s, w, h, 22*s, 0x60000000)
	c.RoundRectGrad(x, y, w, h, 22*s, 0xFF2A2D33, 0xFF1F2125)
	c.RoundRectStroke(x+0.5, y+0.5, w-1, h-1, 22*s, 1, 0x22FFFFFF)

	is := 46 * s
	c.DrawImage(icons["tapboard_logo"], x+24*s, y+22*s, is, is)
	tx := x + 24*s + is + 14*s
	c.Text("Novedades", semiFont, 22*s, semiStyle, colWhite, RectF{tx, y + 20*s, w - (tx - x) - 20*s, 28 * s}, AlignNear, AlignCenter)
	c.Text("TapBoard "+wnVersion, textFont, 14*s, FontStyleRegular, 0xB3FFFFFF, RectF{tx, y + 46*s, w - (tx - x) - 20*s, 22 * s}, AlignNear, AlignCenter)

	ly := y + 92*s
	for _, it := range wnItems {
		if ly+rowH > y+h-40*s {
			break
		}
		c.Circle(x+34*s, ly+rowH/2, 3.5*s, sColAccent)
		c.Text(it, textFont, 15*s, FontStyleRegular, 0xF0FFFFFF, RectF{x + 48*s, ly, w - 72*s, rowH}, AlignNear, AlignCenter)
		ly += rowH
	}
	c.Text("Toca para cerrar", textFont, 12.5*s, FontStyleRegular, 0x80FFFFFF, RectF{x, y + h - 38*s, w, 26 * s}, AlignCenter, AlignCenter)
}
