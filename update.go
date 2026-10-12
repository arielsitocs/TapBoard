//go:build windows

package main

// Actualizaciones: revisa la última release en GitHub, avisa en el panel y,
// al tocar "Actualizar", descarga el .exe nuevo, reemplaza el actual y se reinicia.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	updRepo = "arielsitocs/TapBoard"
	updTag  = "__tapboard_update__"
)

const (
	updIdle = iota
	updChecking
	updUpToDate
	updAvailable
	updDownloading
	updFailed
)

type updMsg struct {
	state    int
	version  string
	assetURL string
	pageURL  string
	progress float32
	restart  bool
	err      string
	manual   bool
}

var (
	updCh       = make(chan updMsg, 16)
	updState    = updIdle
	updVersion  string
	updAsset    string
	updPage     string
	updProgress float32
	updBusy     bool
	updNotified string // versión que ya se avisó con el banner
	updLastPct  = -1
)

// versionNewer: ¿a es más nueva que b? ("2.1.0" vs "2.0.0")
func versionNewer(a, b string) bool {
	pa, pb := splitVer(a), splitVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func splitVer(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if j := strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
			p = p[:j]
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

var updClient = &http.Client{Timeout: 30 * time.Second}

func checkLatest(manual bool) {
	updCh <- updMsg{state: updChecking, manual: manual}
	api := "https://api.github.com/repos/" + updRepo + "/releases/latest"
	if u := os.Getenv("TAPBOARD_UPDATE_API"); u != "" {
		api = u // solo para pruebas
	}
	req, _ := http.NewRequest("GET", api, nil)
	req.Header.Set("User-Agent", "TapBoard/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updClient.Do(req)
	if err != nil {
		updCh <- updMsg{state: updFailed, err: "Sin conexión con GitHub", manual: manual}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		updCh <- updMsg{state: updFailed, err: fmt.Sprintf("GitHub respondió %d", resp.StatusCode), manual: manual}
		return
	}
	var rel struct {
		Tag     string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		updCh <- updMsg{state: updFailed, err: "Respuesta inválida", manual: manual}
		return
	}
	asset := ""
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "TapBoard.exe") {
			asset = a.URL
		}
	}
	v := strings.TrimPrefix(rel.Tag, "v")
	if versionNewer(v, appVersion) && asset != "" {
		updCh <- updMsg{state: updAvailable, version: v, assetURL: asset, pageURL: rel.HTMLURL, manual: manual}
	} else {
		updCh <- updMsg{state: updUpToDate, manual: manual}
	}
}

func updateWorker() {
	time.Sleep(20 * time.Second)
	for {
		if cfg.CheckUpdates {
			checkLatest(false)
		}
		time.Sleep(6 * time.Hour)
	}
}

// startUpdate se llama desde la interfaz (banner o configuración).
func startUpdate() {
	if updBusy {
		return
	}
	if updState != updAvailable || updAsset == "" {
		updBusy = true
		go func() { checkLatest(true) }()
		return
	}
	updBusy = true
	updState = updDownloading
	updProgress = 0
	updLastPct = -1
	toast("Descargando actualización…")
	go downloadAndInstall(updAsset)
}

type progressReader struct {
	r          io.Reader
	n, total   int64
	lastReport time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	k, err := p.r.Read(b)
	p.n += int64(k)
	if p.total > 0 && time.Since(p.lastReport) > 150*time.Millisecond {
		p.lastReport = time.Now()
		select {
		case updCh <- updMsg{state: updDownloading, progress: float32(p.n) / float32(p.total)}:
		default:
		}
	}
	return k, err
}

func downloadAndInstall(url string) {
	fail := func(msg string) { updCh <- updMsg{state: updFailed, err: msg, manual: true} }
	exe, err := os.Executable()
	if err != nil {
		fail("No se encontró el ejecutable")
		return
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "TapBoard/"+appVersion)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		fail("No se pudo descargar")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(fmt.Sprintf("Descarga falló (%d)", resp.StatusCode))
		return
	}
	newPath := exe + ".new"
	f, err := os.Create(newPath)
	if err != nil {
		fail("Sin permiso para escribir en la carpeta de TapBoard")
		return
	}
	n, err := io.Copy(f, &progressReader{r: resp.Body, total: resp.ContentLength})
	f.Close()
	if err != nil || n < 1<<20 || (resp.ContentLength > 0 && n != resp.ContentLength) {
		os.Remove(newPath)
		fail("La descarga quedó incompleta")
		return
	}
	// Windows deja renombrar un .exe en uso: el actual pasa a .old y el nuevo toma su lugar.
	oldPath := exe + ".old"
	os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		fail("No se pudo reemplazar TapBoard.exe")
		return
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(oldPath, exe) // deshacer
		os.Remove(newPath)
		fail("No se pudo reemplazar TapBoard.exe")
		return
	}
	updCh <- updMsg{state: updDownloading, progress: 1, restart: true}
}

// receiveUpdates corre en el hilo de la interfaz (desde el tick).
func receiveUpdates() {
	for {
		select {
		case m := <-updCh:
			handleUpdMsg(m)
		default:
			return
		}
	}
}

func handleUpdMsg(m updMsg) {
	switch m.state {
	case updChecking:
		if updState != updAvailable {
			updState = updChecking
		}
		if m.manual {
			toast("Buscando actualizaciones…")
		}
	case updUpToDate:
		updState = updUpToDate
		updBusy = false
		if m.manual {
			toast("TapBoard está al día (v" + appVersion + ")")
		}
	case updAvailable:
		updState = updAvailable
		updVersion, updAsset, updPage = m.version, m.assetURL, m.pageURL
		updBusy = false
		if m.manual {
			toast("Versión " + m.version + " disponible")
		}
		if updNotified != m.version {
			updNotified = m.version
			showUpdateBanner()
		}
	case updDownloading:
		updProgress = m.progress
		if m.restart {
			toast("Reiniciando TapBoard…")
			invalidateAll()
			restartAfterUpdate()
			return
		}
		pct := int(m.progress * 100)
		if pct/5 != updLastPct/5 {
			updLastPct = pct
			toastText = fmt.Sprintf("Descargando actualización… %d%%", pct)
			toastUntil = time.Now().Add(toastDur * 3)
			startAnim()
		}
	case updFailed:
		wasDownloading := updState == updDownloading
		updBusy = false
		if wasDownloading && updPage != "" {
			// plan B: abrir la página de la versión para descargarla a mano
			pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(updPage), 0, 0, SW_SHOWNORMAL)
			m.err += " · abriendo GitHub"
		}
		if updVersion != "" {
			updState = updAvailable
		} else {
			updState = updFailed
		}
		if m.manual {
			toast(m.err)
		}
	}
	invalidateAll()
}

func showUpdateBanner() {
	notifCur = Notif{Title: "TapBoard " + updVersion + " disponible", Body: "Toca para actualizar", Tag: updTag}
	notifMore = 0
	notifDX, notifDY, notifFly = 0, 0, 0
	notifShown = true
	notifUntil = time.Now().Add(30 * time.Second)
	startAnim()
}

func isUpdateBanner() bool { return notifCur.Tag == updTag }

func invalidateAll() {
	invalidate(hwnd)
	if setHwnd != 0 {
		invalidate(setHwnd)
	}
}

var instanceMutex uintptr

func releaseInstanceMutex() {
	if instanceMutex != 0 {
		pCloseHandle.Call(instanceMutex)
		instanceMutex = 0
	}
}

func restartAfterUpdate() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Soltar el mutex de instancia única para que la nueva versión arranque.
	releaseInstanceMutex()
	if exec.Command(exe, "--after-update").Start() == nil {
		pPostQuitMessage.Call(0)
	}
}

// cleanupOldExe borra el TapBoard.exe.old que dejó la actualización anterior.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(old); err != nil {
			return
		}
		if os.Remove(old) == nil {
			return
		}
		time.Sleep(time.Second)
	}
}

func justUpdated() bool {
	for _, a := range os.Args[1:] {
		if a == "--after-update" {
			return true
		}
	}
	return false
}

// estado para la pestaña Sistema
func updStatusText() string {
	switch updState {
	case updChecking:
		return "Buscando…"
	case updUpToDate:
		return "Estás en la última versión"
	case updAvailable:
		return "Hay una versión nueva: " + updVersion
	case updDownloading:
		return fmt.Sprintf("Descargando… %d%%", int(updProgress*100))
	case updFailed:
		return "No se pudo revisar"
	}
	return ""
}
