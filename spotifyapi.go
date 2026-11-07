//go:build windows

package main

// Cuenta de Spotify conectada: trae todas las playlists del usuario y las pone
// a sonar directamente (Spotify Premium). Inicio de sesión con OAuth + PKCE:
// no hay contraseña ni secreto guardado en la app; solo el "Client ID" de la
// app que el usuario creó en developer.spotify.com y un token de renovación
// guardado cifrado con DPAPI (solo este usuario de Windows puede leerlo).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	spRedirect = "http://127.0.0.1:43821/callback"
	spScopes   = "playlist-read-private playlist-read-collaborative user-read-playback-state user-modify-playback-state"
)

var (
	spMu         sync.Mutex
	spAccess     string
	spAccessExp  time.Time
	spConnecting bool
	spAuthURL    string // página de inicio de sesión abierta (para volver a abrirla)
	spUser       string // nombre visible de la cuenta conectada
	spLists      []Playlist
	spFetched    time.Time
	spCh         = make(chan func(), 8) // cambios para aplicar en el hilo de la interfaz
	spClient     = &http.Client{Timeout: 20 * time.Second}
)

func spTokenPath() string { return filepath.Join(configDir(), "spotify.token") }

func spotifyConnected() bool {
	_, err := os.Stat(spTokenPath())
	return err == nil
}

// ---------------------------------------------------------------- DPAPI

var (
	crypt32             = syscall.NewLazyDLL("crypt32.dll")
	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

type dataBlob struct {
	n uint32
	p *byte
}

func dpapi(proc *syscall.LazyProc, in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("vacío")
	}
	src := dataBlob{uint32(len(in)), &in[0]}
	var dst dataBlob
	r, _, err := proc.Call(uintptr(unsafe.Pointer(&src)), 0, 0, 0, 0, 0x1, uintptr(unsafe.Pointer(&dst))) // UI_FORBIDDEN
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(dst.p)))
	return append([]byte(nil), unsafe.Slice(dst.p, dst.n)...), nil
}

func saveRefreshToken(t string) {
	b, err := dpapi(pCryptProtectData, []byte(t))
	if err != nil {
		return
	}
	os.MkdirAll(configDir(), 0o755)
	os.WriteFile(spTokenPath(), b, 0o600)
}

func loadRefreshToken() string {
	b, err := os.ReadFile(spTokenPath())
	if err != nil {
		return ""
	}
	out, err := dpapi(pCryptUnprotectData, b)
	if err != nil {
		return ""
	}
	return string(out)
}

// ---------------------------------------------------------------- inicio de sesión

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// connectSpotify abre el navegador para que el usuario acepte, y espera la
// respuesta en un servidor local que solo escucha en 127.0.0.1.
func connectSpotify() {
	id := strings.TrimSpace(cfg.SpotifyClientID)
	if id == "" {
		toast("Primero pega el Client ID de tu app de Spotify")
		return
	}
	spMu.Lock()
	if spConnecting {
		// ya está esperando: volver a abrir la página por si quedó escondida
		u := spAuthURL
		spMu.Unlock()
		if u != "" {
			pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(u), 0, 0, SW_SHOWNORMAL)
			toast("Acepta en el navegador para conectar Spotify")
		}
		return
	}
	spConnecting = true
	spMu.Unlock()

	verifier := randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(16)

	ln, err := net.Listen("tcp", "127.0.0.1:43821")
	if err != nil {
		spMu.Lock()
		spConnecting = false
		spMu.Unlock()
		toast("No pude abrir el puerto 43821 para Spotify")
		return
	}
	done := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if q.Get("state") != state || q.Get("code") == "" {
			io.WriteString(w, spPage("No se pudo conectar", "Vuelve a TapBoard e inténtalo otra vez."))
			select {
			case done <- "":
			default:
			}
			return
		}
		io.WriteString(w, spPage("¡Listo!", "TapBoard ya está conectado a tu Spotify. Puedes cerrar esta pestaña."))
		select {
		case done <- q.Get("code"):
		default:
		}
	})}
	go srv.Serve(ln)

	auth := "https://accounts.spotify.com/authorize?" + url.Values{
		"client_id":             {id},
		"response_type":         {"code"},
		"redirect_uri":          {spRedirect},
		"scope":                 {spScopes},
		"state":                 {state},
		"code_challenge_method": {"S256"},
		"code_challenge":        {challenge},
	}.Encode()
	spMu.Lock()
	spAuthURL = auth
	spMu.Unlock()
	pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(auth), 0, 0, SW_SHOWNORMAL)
	toast("Acepta en el navegador para conectar Spotify")

	go func() {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			srv.Shutdown(ctx)
			cancel()
			spMu.Lock()
			spConnecting, spAuthURL = false, ""
			spMu.Unlock()
			spCh <- invalidateAll // la configuración deja de decir "Esperando…"
		}()
		var code string
		select {
		case code = <-done:
		case <-time.After(5 * time.Minute):
		}
		if code == "" {
			spCh <- func() { toast("No se conectó Spotify") }
			return
		}
		tok, err := spTokenRequest(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {spRedirect},
			"client_id":     {id},
			"code_verifier": {verifier},
		})
		if err != nil {
			spCh <- func() { toast("Spotify rechazó la conexión") }
			return
		}
		saveRefreshToken(tok.Refresh)
		refreshSpotifyLists(true)
	}()
}

func spPage(title, msg string) string {
	return `<!doctype html><meta charset="utf-8"><title>TapBoard</title>` +
		`<body style="background:#0B0C0F;color:#EDEEF1;font-family:Segoe UI,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">` +
		`<div style="text-align:center"><h1 style="font-weight:600">` + title + `</h1><p style="color:#8B909A">` + msg + `</p></div></body>`
}

func disconnectSpotify() {
	os.Remove(spTokenPath())
	spMu.Lock()
	spAccess, spUser, spLists = "", "", nil
	spMu.Unlock()
	toast("Spotify desconectado")
}

type spToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expires int    `json:"expires_in"`
}

func spTokenRequest(form url.Values) (spToken, error) {
	var t spToken
	req, _ := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := spClient.Do(req)
	if err != nil {
		return t, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return t, fmt.Errorf("token: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&t); err != nil {
		return t, err
	}
	spMu.Lock()
	spAccess = t.Access
	spAccessExp = time.Now().Add(time.Duration(t.Expires-60) * time.Second)
	spMu.Unlock()
	return t, nil
}

// spAccessToken devuelve un token válido, renovándolo si hace falta.
func spAccessToken() (string, error) {
	spMu.Lock()
	tok, exp := spAccess, spAccessExp
	spMu.Unlock()
	if tok != "" && time.Now().Before(exp) {
		return tok, nil
	}
	rt := loadRefreshToken()
	if rt == "" || cfg.SpotifyClientID == "" {
		return "", fmt.Errorf("sin sesión")
	}
	t, err := spTokenRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rt},
		"client_id":     {strings.TrimSpace(cfg.SpotifyClientID)},
	})
	if err != nil {
		return "", err
	}
	if t.Refresh != "" && t.Refresh != rt {
		saveRefreshToken(t.Refresh) // Spotify puede rotar el token
	}
	return t.Access, nil
}

// spAPI hace una llamada a la API web de Spotify.
func spAPI(method, path string, body any, out any) (int, error) {
	tok, err := spAccessToken()
	if err != nil {
		return 0, err
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(u, "https://") {
		u = "https://api.spotify.com/v1" + path
	}
	if !strings.HasPrefix(u, "https://api.spotify.com/") {
		return 0, fmt.Errorf("dirección no permitida")
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := spClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == 200 {
		json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}
	return resp.StatusCode, nil
}

// ---------------------------------------------------------------- playlists

// refreshSpotifyLists trae las playlists de la cuenta (hasta 200). Se llama al
// conectar y al abrir la lista, como mucho cada 5 minutos salvo force.
func refreshSpotifyLists(force bool) {
	if !spotifyConnected() {
		return
	}
	spMu.Lock()
	recent := time.Since(spFetched) < 5*time.Minute
	spMu.Unlock()
	if recent && !force {
		return
	}
	go func() {
		var me struct {
			Name string `json:"display_name"`
		}
		if code, err := spAPI("GET", "/me", nil, &me); err != nil || code != 200 {
			if code == 401 || code == 403 {
				spCh <- func() { toast("Vuelve a conectar Spotify") }
			}
			return
		}
		var all []Playlist
		next := "/me/playlists?limit=50"
		for next != "" && len(all) < 200 {
			var page struct {
				Items []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"items"`
				Next string `json:"next"`
			}
			if code, err := spAPI("GET", next, nil, &page); err != nil || code != 200 {
				break
			}
			for _, it := range page.Items {
				if it.ID == "" {
					continue
				}
				p := Playlist{Kind: "playlist", ID: it.ID, Name: it.Name, Auto: true}
				if len(it.Images) > 0 {
					p.thumb = it.Images[len(it.Images)/2].URL // la mediana: ni enorme ni chica
				}
				all = append(all, p)
			}
			next = page.Next
		}
		// portadas que todavía no están en disco
		for _, p := range all {
			if _, err := os.Stat(p.coverPath()); err == nil || p.thumb == "" {
				continue
			}
			downloadCover(p, p.thumb)
		}
		spCh <- func() {
			spMu.Lock()
			spUser, spLists, spFetched = me.Name, all, time.Now()
			spMu.Unlock()
			for _, p := range all {
				delete(plCovers, p.Kind+":"+p.ID)
			}
			invalidateAll()
		}
	}()
}

// receiveSpotify aplica en el hilo de la interfaz lo que llegó de Spotify.
func receiveSpotify() {
	for {
		select {
		case fn := <-spCh:
			fn()
		default:
			return
		}
	}
}

// allPlaylists: las de la cuenta conectada y las agregadas con link, sin repetir.
func allPlaylists() []Playlist {
	spMu.Lock()
	out := append([]Playlist(nil), spLists...)
	spMu.Unlock()
	seen := map[string]bool{}
	for _, p := range out {
		seen[p.Kind+":"+p.ID] = true
	}
	for _, p := range cfg.Playlists {
		if !seen[p.Kind+":"+p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------- reproducir

// spotifyPlay pone a sonar la playlist en la cuenta conectada. Si Spotify no
// está abierto en ningún dispositivo, abre la app de escritorio y vuelve a
// intentar en este PC.
func spotifyPlay(p Playlist) bool {
	if !spotifyConnected() {
		return false
	}
	go func() {
		body := map[string]string{"context_uri": p.uri()}
		code, err := spAPI("PUT", "/me/player/play", body, nil)
		if err == nil && (code == 204 || code == 202 || code == 200) {
			spCh <- func() { toast("Sonando: " + playlistName(p)) }
			return
		}
		if code == 403 {
			spCh <- func() { toast("Para reproducir desde TapBoard hace falta Spotify Premium") }
			return
		}
		// sin dispositivo activo: abrir Spotify y esperar a que aparezca
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(p.uri()), 0, 0, SW_SHOWNORMAL)
		for try := 0; try < 8; try++ {
			time.Sleep(1500 * time.Millisecond)
			var devs struct {
				Devices []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"devices"`
			}
			if c, err := spAPI("GET", "/me/player/devices", nil, &devs); err != nil || c != 200 {
				continue
			}
			dev := ""
			for _, d := range devs.Devices {
				if strings.EqualFold(d.Type, "Computer") {
					dev = d.ID
				}
			}
			if dev == "" && len(devs.Devices) > 0 {
				dev = devs.Devices[0].ID
			}
			if dev == "" {
				continue
			}
			if c, err := spAPI("PUT", "/me/player/play?device_id="+url.QueryEscape(dev), body, nil); err == nil && (c == 204 || c == 202 || c == 200) {
				spCh <- func() { toast("Sonando: " + playlistName(p)) }
				return
			}
		}
		spCh <- func() { toast("Abrí " + playlistName(p) + " en Spotify; toca play") }
	}()
	return true
}
