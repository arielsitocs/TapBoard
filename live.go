//go:build windows

package main

import (
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- Spotify

var (
	spot        SpotifyState
	spotSong    string
	spotArtist  string
	spotWasPlay bool
)

func updateSpotify() {
	var st SpotifyState
	if demo {
		st = SpotifyState{Running: true, Playing: !playPaused, Song: "Canción de prueba", Artist: "Artista de prueba"}
	} else {
		st = pollSpotify(tickCount%30 == 0)
	}
	changed := st != spot
	if st.Playing && st.Song != "" {
		spotSong, spotArtist = st.Song, st.Artist
	}
	if !st.Running {
		spotSong, spotArtist = "", ""
	}
	// Sincroniza el botón play/pausa con Spotify (solo cuando cambia).
	if st.Running && (st.Playing != spotWasPlay || !spot.Running) && !demo {
		playPaused = !st.Playing
		changed = true
	}
	spotWasPlay = st.Playing
	spot = st
	if changed {
		invalidate(hwnd)
	}
}

// ---------------------------------------------------------------- WhatsApp

var (
	notifCh      = make(chan []Notif, 8)
	waEnabled    atomic.Bool
	waReset      atomic.Bool
	notifCur     Notif
	notifMore    int
	notifShown   bool
	notifUntil   time.Time
	notifAnim    float32
	notifPressed bool
	notifDX      float32
	notifDY      float32
	notifFly     int
)

const notifDur = 8 * time.Second

func resetWhatsApp() {
	waEnabled.Store(cfg.WhatsApp)
	waReset.Store(true)
}

func whatsappWatcher() {
	path := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Notifications", "wpndatabase.db")
	since := int64(-1)
	var lastSig string
	for {
		time.Sleep(1500 * time.Millisecond)
		if waReset.Swap(false) {
			since = -1
			lastSig = ""
		}
		if !waEnabled.Load() {
			continue
		}
		sig := ""
		for _, p := range []string{path, path + "-wal"} {
			if st, err := os.Stat(p); err == nil {
				sig += st.ModTime().String() + "|" + string(rune(st.Size()%1000003))
			}
		}
		if sig == "" || sig == lastSig {
			continue
		}
		lastSig = sig
		base := since
		if base < 0 {
			base = 0
		}
		list, maxA, err := readWhatsApp(path, base)
		if err != nil {
			lastSig = "" // reintentar
			continue
		}
		addHistory(list)
		if since >= 0 && len(list) > 0 {
			select {
			case notifCh <- list:
			default:
			}
		}
		since = maxA
	}
}

func receiveNotifs() {
	for {
		select {
		case list := <-notifCh:
			if !cfg.WhatsApp || len(list) == 0 {
				continue
			}
			list = dedupeNotifs(list)
			if len(list) == 0 {
				continue
			}
			last := list[len(list)-1]
			replaces := notifShown && isGenericNotif(notifCur) && sameSender(notifCur, last)
			if notifShown && isGenericNotif(last) && sameSender(notifCur, last) {
				continue // el aviso genérico de un mensaje que ya se muestra
			}
			if replaces {
				notifMore += len(list) - 1
			} else if notifShown {
				notifMore += len(list)
			} else {
				notifMore = len(list) - 1
			}
			notifCur = last
			notifDX, notifDY, notifFly = 0, 0, 0
			notifShown = true
			notifUntil = time.Now().Add(notifDur)
			lastTouch = time.Now() // un mensaje nuevo cuenta como actividad
			startAnim()
		default:
			return
		}
	}
}

func notifRect() RectF {
	h := tiles[tSpeaker].H * 0.82
	e := 1 - (1-notifAnim)*(1-notifAnim)
	y := pad - (1-e)*(h+pad*3)
	return RectF{pad + notifDX, y + notifDY, panelW - 2*pad, h}
}

func paintNotif(c *Canvas) {
	if notifAnim < 0.01 {
		return
	}
	r := notifRect()
	rad := min32(r.W, r.H) * 0.3
	// se desvanece al arrastrarla hacia un lado
	fade := 1 - math.Min(1, math.Abs(float64(notifDX))/float64(panelW*0.7))
	if fade < 1 {
		c.SetAlpha(float32(fade))
		defer c.SetAlpha(1)
	}
	c.RoundRect(r.X, r.Y+r.H*0.06, r.W, r.H, rad, 0x66000000) // sombra
	top, bot := uint32(0xFF1F6B47), uint32(0xFF16523A)
	icon := icons["speech_balloon"]
	if isUpdateBanner() {
		top, bot = 0xFF2F63E0, 0xFF2346B0
		icon = icons["tapboard_logo"]
	}
	if notifPressed {
		top, bot = lighten(top, 0.1), lighten(bot, 0.1)
	}
	c.RoundRectGrad(r.X, r.Y, r.W, r.H, rad, top, bot)
	is := r.H * 0.56
	ix := r.X + r.H*0.24
	c.DrawImage(icon, ix, r.Y+(r.H-is)/2, is, is)
	tx := ix + is + r.H*0.2
	tw := r.X + r.W - tx - r.H*0.3
	more := ""
	if notifMore > 0 {
		more = "+" + itoa(notifMore)
		mw := r.H * 0.55
		tw -= mw
		c.RoundRect(r.X+r.W-r.H*0.25-mw, r.Y+(r.H-r.H*0.36)/2, mw, r.H*0.36, r.H*0.18, 0x40FFFFFF)
		c.Text(more, semiFont, r.H*0.18, semiStyle, colWhite,
			RectF{r.X + r.W - r.H*0.25 - mw, r.Y + (r.H-r.H*0.36)/2, mw, r.H * 0.36}, AlignCenter, AlignCenter)
	}
	body := notifCur.Body
	if !isUpdateBanner() && (!cfg.WhatsAppTexto || body == "") {
		body = "Nuevo mensaje"
	}
	c.Text(notifCur.Title, semiFont, r.H*0.22, semiStyle, colWhite, RectF{tx, r.Y + r.H*0.14, tw, r.H * 0.36}, AlignNear, AlignCenter)
	c.Text(body, textFont, r.H*0.19, FontStyleRegular, 0xD9FFFFFF, RectF{tx, r.Y + r.H*0.5, tw, r.H * 0.34}, AlignNear, AlignCenter)
}

func itoa(n int) string {
	if n > 99 {
		return "99"
	}
	s := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func notifHit(x, y int32) bool {
	return notifShown && notifAnim > 0.5 && inRect(notifRect(), float32(x), float32(y))
}

func openWhatsApp() {
	r, _, _ := pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("whatsapp:"), 0, 0, SW_SHOWNORMAL)
	if r <= 32 {
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("https://web.whatsapp.com"), 0, 0, SW_SHOWNORMAL)
	}
}
