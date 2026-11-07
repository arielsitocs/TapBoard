//go:build windows

package main

// Playlists de Spotify elegidas por el usuario: se agregan pegando el link de
// compartir, TapBoard saca el nombre y la portada de la página pública de
// Spotify (oEmbed, sin iniciar sesión) y se abren con un toque desde la
// tarjeta de música o desde un botón.

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// playlistCol: color de los botones de playlist (verde Spotify, oscurecido
// como los demás colores de botón).
const playlistCol = 0xFF1E5A3A

// Playlist guardada en la configuración.
type Playlist struct {
	Kind string `json:"kind"` // "playlist" o "album"
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`

	Auto  bool   `json:"-"` // vino de la cuenta de Spotify conectada
	thumb string // dirección de la portada (solo al traerla de la cuenta)
}

func (p Playlist) uri() string    { return "spotify:" + p.Kind + ":" + p.ID }
func (p Playlist) webURL() string { return "https://open.spotify.com/" + p.Kind + "/" + p.ID }
func (p Playlist) coverPath() string {
	return filepath.Join(configDir(), "covers", p.Kind+"_"+p.ID+".jpg")
}

// parseSpotifyLink entiende "https://open.spotify.com/playlist/ID?si=…",
// "https://open.spotify.com/intl-es/album/ID" y "spotify:playlist:ID".
func parseSpotifyLink(s string) (Playlist, bool) {
	s = strings.TrimSpace(s)
	var parts []string
	if strings.HasPrefix(s, "spotify:") {
		parts = strings.Split(s, ":")[1:]
	} else {
		u, err := url.Parse(s)
		if err != nil || !strings.HasSuffix(strings.ToLower(u.Host), "spotify.com") {
			return Playlist{}, false
		}
		for _, p := range strings.Split(strings.Trim(u.Path, "/"), "/") {
			if p != "" && !strings.HasPrefix(p, "intl-") {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) < 2 || (parts[0] != "playlist" && parts[0] != "album") {
		return Playlist{}, false
	}
	id := parts[1]
	if len(id) < 10 || len(id) > 40 {
		return Playlist{}, false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return Playlist{}, false
		}
	}
	return Playlist{Kind: parts[0], ID: id}, true
}

// ---------------------------------------------------------------- portapapeles

var (
	pOpenClipboard    = user32.NewProc("OpenClipboard")
	pCloseClipboard   = user32.NewProc("CloseClipboard")
	pGetClipboardData = user32.NewProc("GetClipboardData")
	pGlobalLock       = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalLock")
	pGlobalUnlock     = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalUnlock")
)

func clipboardText() string {
	if r, _, _ := pOpenClipboard.Call(hwnd); r == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(13) // CF_UNICODETEXT
	if h == 0 {
		return ""
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(h)
	var u []uint16
	for i := 0; i < 4096; i++ {
		ch := *(*uint16)(unsafe.Pointer(p + uintptr(i)*2))
		if ch == 0 {
			break
		}
		u = append(u, ch)
	}
	return syscall.UTF16ToString(u)
}

var (
	pEmptyClipboard   = user32.NewProc("EmptyClipboard")
	pSetClipboardData = user32.NewProc("SetClipboardData")
	pGlobalAlloc      = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalAlloc")
)

// setClipboardText copia un texto al portapapeles.
func setClipboardText(s string) bool {
	u, _ := syscall.UTF16FromString(s)
	if r, _, _ := pOpenClipboard.Call(hwnd); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	h, _, _ := pGlobalAlloc.Call(0x0002, uintptr(len(u)*2)) // GMEM_MOVEABLE
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return false
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pGlobalUnlock.Call(h)
	r, _, _ := pSetClipboardData.Call(13, h) // CF_UNICODETEXT; Windows se queda con la memoria
	return r != 0
}

// pasteSpotifyClientID toma el Client ID copiado (32 caracteres hexadecimales).
func pasteSpotifyClientID() {
	id := strings.TrimSpace(clipboardText())
	ok := len(id) == 32
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			ok = false
		}
	}
	if !ok {
		toast("Copia el Client ID desde tu app en developer.spotify.com")
		return
	}
	cfg.SpotifyClientID = id
	saveConfig(cfg)
	toast("Client ID guardado")
}

// ---------------------------------------------------------------- agregar

type plMeta struct {
	key  string
	name string
	ok   bool
}

var plCh = make(chan plMeta, 8)

// addPlaylistFromClipboard agrega la playlist cuyo link está copiado.
func addPlaylistFromClipboard() {
	p, ok := parseSpotifyLink(clipboardText())
	if !ok {
		toast("Copia el link en Spotify: Compartir → Copiar enlace")
		return
	}
	for _, q := range cfg.Playlists {
		if q.Kind == p.Kind && q.ID == p.ID {
			toast("Esa playlist ya está agregada")
			return
		}
	}
	cfg.Playlists = append(cfg.Playlists, p)
	saveConfig(cfg)
	toast("Buscando la playlist…")
	go fetchPlaylistMeta(p)
}

func removePlaylist(i int) {
	if i < 0 || i >= len(cfg.Playlists) {
		return
	}
	p := cfg.Playlists[i]
	cfg.Playlists = append(cfg.Playlists[:i], cfg.Playlists[i+1:]...)
	saveConfig(cfg)
	os.Remove(p.coverPath())
	delete(plCovers, p.Kind+":"+p.ID)
}

var plClient = &http.Client{Timeout: 15 * time.Second}

// fetchPlaylistMeta trae nombre y portada desde la página pública de Spotify.
func fetchPlaylistMeta(p Playlist) {
	key := p.Kind + ":" + p.ID
	api := "https://open.spotify.com/oembed?url=" + url.QueryEscape(p.webURL())
	req, _ := http.NewRequest("GET", api, nil)
	req.Header.Set("User-Agent", "TapBoard/"+appVersion)
	resp, err := plClient.Do(req)
	if err != nil {
		plCh <- plMeta{key: key}
		return
	}
	defer resp.Body.Close()
	var m struct {
		Title string `json:"title"`
		Thumb string `json:"thumbnail_url"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m) != nil {
		plCh <- plMeta{key: key}
		return
	}
	downloadCover(p, m.Thumb)
	plCh <- plMeta{key: key, name: m.Title, ok: true}
}

// downloadCover guarda la portada en disco. Solo se baja de los servidores de
// imágenes de Spotify.
func downloadCover(p Playlist, thumb string) {
	u, err := url.Parse(thumb)
	if err != nil || u.Scheme != "https" ||
		!(strings.HasSuffix(u.Host, ".scdn.co") || strings.HasSuffix(u.Host, ".spotifycdn.com")) {
		return
	}
	r, err := plClient.Get(thumb)
	if err != nil {
		return
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	r.Body.Close()
	if r.StatusCode == 200 && len(b) > 0 {
		os.MkdirAll(filepath.Dir(p.coverPath()), 0o755)
		os.WriteFile(p.coverPath(), b, 0o644)
	}
}

// receivePlaylists corre en el hilo de la interfaz (desde el tick).
func receivePlaylists() {
	for {
		select {
		case m := <-plCh:
			for i, p := range cfg.Playlists {
				if p.Kind+":"+p.ID == m.key {
					if m.ok && m.name != "" {
						cfg.Playlists[i].Name = m.name
						toast("Agregada: " + m.name)
					} else if p.Name == "" {
						toast("No pude leer la playlist (¿es pública?)")
					}
					delete(plCovers, m.key)
					saveConfig(cfg)
				}
			}
			invalidateAll()
		default:
			return
		}
	}
}

// ---------------------------------------------------------------- portadas

var plCovers = map[string]*Image{}

func playlistCover(p Playlist) *Image {
	key := p.Kind + ":" + p.ID
	if img, ok := plCovers[key]; ok {
		return img
	}
	var img *Image
	if b, err := os.ReadFile(p.coverPath()); err == nil {
		if dec, _, err := image.Decode(bytes.NewReader(b)); err == nil {
			w, h, pix := squareRGBA(dec, 240)
			img = newImage(w, h, pix)
		}
	}
	plCovers[key] = img // nil también se recuerda: no reintentar en cada cuadro
	return img
}

func playlistName(p Playlist) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Kind == "album" {
		return "Álbum"
	}
	return "Playlist"
}

// ---------------------------------------------------------------- reproducir

// playPlaylist la pone a sonar con la cuenta conectada; sin cuenta, la abre en
// Spotify de escritorio (o en la web si no está instalado).
func playPlaylist(p Playlist) {
	if spotifyPlay(p) {
		toast("Poniendo " + playlistName(p) + "…")
		return
	}
	r, _, _ := pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(p.uri()), 0, 0, SW_SHOWNORMAL)
	if r <= 32 {
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(p.webURL()), 0, 0, SW_SHOWNORMAL)
	}
	toast("Abriendo " + playlistName(p) + " en Spotify")
}

func playlistByURL(u string) (Playlist, bool) {
	p, ok := parseSpotifyLink(u)
	if !ok {
		return p, false
	}
	for _, q := range allPlaylists() {
		if q.Kind == p.Kind && q.ID == p.ID {
			return q, true
		}
	}
	return p, true
}

// ---------------------------------------------------------------- lista

var (
	plOpen      bool
	plAnim      float32
	plHits      []sHit
	plPressed   = -1
	plScroll    float32
	plContent   float32
	plDownY     int32
	plDownScr   float32
	plScrolling bool
)

func openPlaylists() {
	plScroll, plPressed = 0, -1
	plOpen = true
	refreshSpotifyLists(false) // trae las nuevas de la cuenta, si está conectada
	pageSound()
	startAnim()
}

func closePlaylists() {
	plOpen = false
	plPressed = -1
	startAnim()
}

func animPlaylists() bool {
	t := float32(0)
	if plOpen {
		t = 1
	}
	if d := t - plAnim; math.Abs(float64(d)) > 0.005 {
		plAnim += d * 0.24
		return true
	}
	plAnim = t
	return false
}

func paintPlaylists(c *Canvas) {
	if plAnim < 0.01 {
		return
	}
	s := panelSetScale()
	e := 1 - (1-plAnim)*(1-plAnim)*(1-plAnim)
	oy := (1 - e) * panelH * 0.25
	c.ResetTransform()
	c.SetAlpha(e)
	defer c.SetAlpha(1)
	c.RoundRect(-2, -2, panelW+4, panelH+4, 0, 0xF60B0C0F)
	c.Translate(0, oy-plScroll)
	plHits = nil
	hit := func(r RectF, fn func()) bool {
		plHits = append(plHits, sHit{r, fn})
		return plPressed == len(plHits)-1
	}
	px := pad * 1.6
	y := 18 * s
	// encabezado
	c.Text("Playlists", semiFont, 22*s, semiStyle, gText, RectF{px, y, panelW * 0.5, 34 * s}, AlignNear, AlignCenter)
	bh := 34 * s
	b1 := RectF{panelW - px - 96*s, y, 96 * s, bh}
	col := uint32(gRaised)
	if hit(b1, closePlaylists) {
		col = lighten(col, 0.12)
	}
	c.RoundRect(b1.X, b1.Y, b1.W, b1.H, bh/2, col)
	c.Text("Cerrar", semiFont, 14*s, semiStyle, gText, b1, AlignCenter, AlignCenter)
	b2 := RectF{b1.X - 10*s - 150*s, y, 150 * s, bh}
	col = gAccent
	if hit(b2, addPlaylistFromClipboard) {
		col = lighten(col, 0.12)
	}
	c.RoundRect(b2.X, b2.Y, b2.W, b2.H, bh/2, col)
	c.Text("+ Pegar link", semiFont, 14*s, semiStyle, gInk, b2, AlignCenter, AlignCenter)
	y += bh + 18*s

	if spotifyConnected() {
		spMu.Lock()
		who := spUser
		spMu.Unlock()
		sub := "Spotify conectado"
		if who != "" {
			sub += " · " + who
		}
		c.Text(sub, textFont, 13*s, FontStyleRegular, gMuted, RectF{px + 120*s, 18 * s, panelW * 0.3, 34 * s}, AlignNear, AlignCenter)
	}
	list := allPlaylists()
	if len(list) == 0 {
		msg := "En Spotify toca Compartir → Copiar enlace en una playlist o álbum, y después «+ Pegar link». O conecta tu cuenta en Configuración → Música para verlas todas."
		if spotifyConnected() {
			msg = "Buscando tus playlists en Spotify…"
		}
		c.Text("Todavía no hay playlists.", semiFont, 17*s, semiStyle, gText, RectF{px, y + 30*s, panelW - 2*px, 28 * s}, AlignCenter, AlignCenter)
		c.TextWrap(msg, textFont, 14*s, FontStyleRegular, gMuted, RectF{panelW * 0.15, y + 64*s, panelW * 0.7, 60 * s}, AlignCenter, AlignNear)
		plContent = panelH
		c.ResetTransform()
		return
	}
	cols := 5
	gap := 12 * s
	cw := (panelW - 2*px - gap*float32(cols-1)) / float32(cols)
	ch := cw + 44*s
	for i, p := range list {
		p := p
		x := px + float32(i%cols)*(cw+gap)
		yy := y + float32(i/cols)*(ch+gap)
		r := RectF{x, yy, cw, ch}
		pr := hit(r, func() { playPlaylist(p); closePlaylists() })
		bg := uint32(gSurface)
		if pr {
			bg = lighten(bg, 0.1)
		}
		c.RoundRect(r.X, r.Y, r.W, r.H, gRadius(r)*0.7, bg)
		in := 8 * s
		ar := RectF{r.X + in, r.Y + in, cw - 2*in, cw - 2*in}
		if img := playlistCover(p); img != nil {
			c.ClipRoundRect(ar.X, ar.Y, ar.W, ar.H, ar.W*0.06)
			c.drawImageHQ(img, ar.X, ar.Y, ar.W, ar.H)
			c.ResetClip()
		} else {
			c.RoundRect(ar.X, ar.Y, ar.W, ar.H, ar.W*0.06, gRaised)
			is := ar.W * 0.32
			gIcon(c, nil, glyphs["musical_note"], ar.X+(ar.W-is)/2, ar.Y+(ar.H-is)/2, is, gMuted)
		}
		c.TextWrap(playlistName(p), semiFont, 13*s, semiStyle, gText, RectF{r.X + in, ar.Y + ar.H + 6*s, r.W - 2*in, 34 * s}, AlignNear, AlignNear)
	}
	rows := (len(list) + cols - 1) / cols
	plContent = y + float32(rows)*(ch+gap) + 40*s
	c.ResetTransform()
}

func plHit(x, y int32) int {
	fx, fy := float32(x), float32(y)+plScroll // solo se toca con la lista ya abierta
	for i := len(plHits) - 1; i >= 0; i-- {
		if inRect(plHits[i].r, fx, fy) {
			return i
		}
	}
	return -1
}

func clampPl() {
	maxS := plContent - panelH
	if maxS < 0 {
		maxS = 0
	}
	if plScroll > maxS {
		plScroll = maxS
	}
	if plScroll < 0 {
		plScroll = 0
	}
}

func playlistsDown(x, y int32) {
	plDownY, plDownScr, plScrolling = y, plScroll, false
	plPressed = plHit(x, y)
	if plPressed >= 0 {
		clickSound()
	}
	invalidate(hwnd)
}

func playlistsMove(x, y int32) {
	dy := float32(y - plDownY)
	if !plScrolling && math.Abs(float64(dy)) > float64(panelH*0.025) {
		plScrolling = true
		plPressed = -1
	}
	if plScrolling {
		plScroll = plDownScr - dy
		clampPl()
		invalidate(hwnd)
	}
}

func playlistsUp(x, y int32) {
	i := plHit(x, y)
	p := plPressed
	plPressed = -1
	if !plScrolling && i >= 0 && i == p && i < len(plHits) {
		plHits[i].fn()
	}
	plScrolling = false
	invalidate(hwnd)
}
