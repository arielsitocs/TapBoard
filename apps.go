//go:build windows

package main

// Botones configurables de la página 2: acciones del panel o apps instaladas.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	pSHGetKnownFolderItem        = shell32x.NewProc("SHGetKnownFolderItem")
	pSHCreateItemFromParsingName = shell32x.NewProc("SHCreateItemFromParsingName")
	pGetObjectW                  = syscall.NewLazyDLL("gdi32.dll").NewProc("GetObjectW")

	FOLDERID_AppsFolder        = parseGUID("1E87508D-89C2-42F0-8A7E-645A0F50CA58")
	BHID_EnumItems             = parseGUID("94F60519-2850-4924-AA5A-D15E84868039")
	IID_IShellItem             = parseGUID("43826D1E-E718-42EE-BC55-A1E261C37BFE")
	IID_IEnumShellItems        = parseGUID("70629033-E363-4A28-A567-0DB78006E6D7")
	IID_IShellItemImageFactory = parseGUID("BCC18B79-BA16-442F-80C4-8A59C30C463B")
)

// ---------------------------------------------------------------- acciones

type actionDef struct {
	key, icon, label string
	col              uint32
}

var actionDefs = []actionDef{
	{"taskManager", "bar_chart", "Tareas", 0xFF2E3B55},
	{"calculator", "abacus", "Calculadora", 0xFF3D3550},
	{"explorer", "card_index_dividers", "Explorador", 0xFF5A4A2A},
	{"settings", "gear", "Configuración", 0xFF3A3F47},
	{"desktop", "desktop_computer", "Escritorio", 0xFF1F4E5A},
	{"clipboard", "clipboard", "Portapapeles", 0xFF4A3F2E},
	{"emoji", "grinning_face", "Emojis", 0xFF5A4520},
	{"lock", "locked", "Bloquear", 0xFF4A3030},
	{"screenshot", "camera_with_flash", "Captura", 0xFF1F5B5D},
	{"folder", "open_file_folder", "Descargas", 0xFF6D5328},
	{"closeApp", "cross_mark", "Cerrar app", 0xFF4A3030},
	{"sleep", "crescent_moon", "Suspender", 0xFF233650},
	{"shutdown", "stop_sign", "Apagar", 0xFF6E2A33},
	{"empty", "", "Vacío", 0xFF242528},
}

func actionByKey(k string) actionDef {
	for _, a := range actionDefs {
		if a.key == k {
			return a
		}
	}
	return actionDefs[len(actionDefs)-1]
}

func defaultMainButtons() []BotonP2 {
	var b []BotonP2
	for _, k := range []string{"screenshot", "folder", "closeApp", "sleep", "shutdown"} {
		b = append(b, BotonP2{Tipo: "action", Accion: k})
	}
	return b
}

// BotonP2 es la configuración de un botón.
// Tipos: "app", "action", "media", "hotkey", "url", "text", "file", "page".
type BotonP2 struct {
	Tipo   string   `json:"type"`
	Accion string   `json:"action,omitempty"`
	Nombre string   `json:"name,omitempty"`
	Ruta   string   `json:"path,omitempty"`
	Color  string   `json:"color,omitempty"` // "#RRGGBB" (vacío = automático)
	Keys   string   `json:"keys,omitempty"`  // atajo: "Ctrl+Shift+M"
	Text   string   `json:"text,omitempty"`  // texto a escribir
	Page   int      `json:"page,omitempty"`  // página a la que ir (1 = principal)
	Icon   string   `json:"icon,omitempty"`  // ícono elegido (vacío = automático)
	Hold   *BotonP2 `json:"hold,omitempty"`  // acción al mantener presionado
}

func defaultBotones() []BotonP2 {
	var b []BotonP2
	for _, a := range actionDefs[:8] {
		b = append(b, BotonP2{Tipo: "action", Accion: a.key})
	}
	return b
}

func slotCfg(bp, i int) BotonP2 {
	sl := pageSlots(bp)
	if i >= 0 && i < len(sl) {
		return sl[i]
	}
	return BotonP2{Tipo: "action", Accion: "empty"}
}

// slotVisual devuelve ícono, texto y color de un botón de la página 2.
func slotVisual(bp, i int) (*Image, string, uint32) {
	return visualOf(slotCfg(bp, i))
}

// visualOf devuelve ícono, nombre y color de un botón según su tipo.
func visualOf(b BotonP2) (*Image, string, uint32) {
	var img *Image
	var label string
	var col uint32
	switch b.Tipo {
	case "app":
		img, col = appIcon(b.Ruta)
		label = b.Nombre
	case "file":
		img, col = appIcon(b.Ruta)
		label = filepathBase(b.Ruta)
	case "media":
		m := mediaByKey(b.Accion)
		img, label, col = icons[m.icon], m.label, m.col
	case "hotkey":
		img, label, col = icons["keyboard"], b.Keys, 0xFF2E3B55
		if label == "" {
			label = "Atajo"
		}
	case "url":
		img, label, col = icons["globe_with_meridians"], urlHost(b.Ruta), 0xFF1F4E5A
	case "text":
		img, label, col = icons["memo"], "Escribir texto", 0xFF4A3F2E
	case "page":
		img, label, col = icons["right_arrow"], "Página "+itoa(b.Page), 0xFF3D3550
	default:
		a := actionByKey(b.Accion)
		img, label, col = icons[a.icon], a.label, a.col
		if a.key == "folder" && cfg.Carpeta != "" {
			label = filepathBase(cfg.Carpeta)
		}
	}
	if b.Nombre != "" {
		label = b.Nombre
	}
	if b.Icon != "" {
		if ic, ok := icons[b.Icon]; ok {
			img = ic
		}
	}
	if c, ok := parseHexColor(b.Color); ok {
		col = c
	}
	return img, label, col
}

func urlHost(u string) string {
	h := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	h = strings.TrimPrefix(h, "www.")
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if h == "" {
		return "Web"
	}
	return h
}

// ---- multimedia

type mediaDef struct {
	key, icon, label string
	col              uint32
}

var mediaDefs = []mediaDef{
	{"playPause", "play_or_pause_button", "Play / Pausa", 0xFF2E3B55},
	{"nextTrack", "next_track_button", "Siguiente", 0xFF2E3B55},
	{"prevTrack", "last_track_button", "Anterior", 0xFF2E3B55},
	{"volUp", "speaker_high_volume", "Subir volumen", 0xFF1F4E5A},
	{"volDown", "speaker_low_volume", "Bajar volumen", 0xFF1F4E5A},
	{"mute", "muted_speaker", "Silenciar", 0xFF6E2A33},
	{"micMute", "studio_microphone", "Micrófono", 0xFF4A3466},
	{"nextDevice", "headphone", "Cambiar salida", 0xFF4A3466},
}

func mediaByKey(k string) mediaDef {
	for _, m := range mediaDefs {
		if m.key == k {
			return m
		}
	}
	return mediaDefs[0]
}

func runMedia(k string) {
	switch k {
	case "playPause":
		sendMediaPlayPause()
		playPaused = !playPaused
	case "nextTrack":
		sendMediaKey(0xB0)
	case "prevTrack":
		sendMediaKey(0xB1)
	case "volUp":
		sendMediaKey(0xAF)
	case "volDown":
		sendMediaKey(0xAE)
	case "mute":
		sendMediaKey(0xAD)
	case "micMute":
		if !demo {
			audio.ToggleMic()
			refreshAudio(true)
		}
		if aud.MicMuted {
			toast("Micrófono silenciado")
		} else {
			toast("Micrófono activado")
		}
		return
	case "nextDevice":
		if !demo {
			audio.NextDevice(cfg.DispositivosAudio)
			refreshAudio(true)
		}
		toast("Audio: " + ShortName(aud.DeviceName))
		return
	}
	toast(mediaByKey(k).label)
}

func filepathBase(p string) string { return filepath.Base(p) }

func parseHexColor(s string) (uint32, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, false
	}
	var v uint32
	for _, ch := range strings.ToLower(s) {
		v <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			v |= uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			v |= uint32(ch-'a') + 10
		default:
			return 0, false
		}
	}
	return 0xFF000000 | v, true
}

// drawSlot dibuja un botón configurable (página 1 fila de abajo o páginas de botones).
func drawSlot(c *Canvas, bp, s int) {
	id := slotID(bp, s)
	if id == reorderID {
		r := rectOf(id)
		c.RoundRect(r.X, r.Y, r.W, r.H, radius(r), 0xFF1C1D20)
		return
	}
	img, cap, col := slotVisual(bp, s)
	if bp >= 0 {
		// en las páginas de botones el texto siempre se ve
	} else {
		cap = "" // en la página 1, solo ícono (como antes)
	}
	if t := flashText(id); t != "" {
		cap = t
	} else if pendingTile == id && time.Now().Before(pendingUntil) {
		if slotCfg(bp, s).Accion == "closeApp" && slotCfg(bp, s).Tipo != "app" {
			cap = "¿Cerrar " + windowTitleShort(closeTarget) + "?"
		} else {
			cap = "Toca otra vez"
		}
	}
	if reorderID >= 0 && hitTest(lastPX, lastPY) == id {
		col = lighten(col, 0.18)
	}
	drawImgTile(c, id, col, img, cap)
	if h := slotCfg(bp, s).Hold; h != nil && h.Tipo != "" {
		// puntito: este botón tiene una acción al mantener presionado
		r := rectOf(id)
		d := min32(r.W, r.H)
		c.Circle(r.X+r.W-d*0.11, r.Y+d*0.11, d*0.032, 0xB3FFFFFF)
	}
}

var runningHold bool

func runSlot(bp, i int) { runButton(slotCfg(bp, i), bp, i) }

// runHold ejecuta la acción de "mantener presionado" de un botón (si tiene).
func runHold(bp, i int) bool {
	h := slotCfg(bp, i).Hold
	if h == nil || h.Tipo == "" || (h.Tipo == "action" && h.Accion == "empty") {
		return false
	}
	runningHold = true
	runButton(*h, bp, i)
	runningHold = false
	return true
}

func runButton(b BotonP2, bp, i int) {
	_, label, _ := visualOf(b)
	switch b.Tipo {
	case "app", "file":
		if b.Ruta != "" {
			pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(b.Ruta), 0, 0, SW_SHOWNORMAL)
			toast("Abriendo " + label + "…")
		}
		return
	case "url":
		u := strings.TrimSpace(b.Ruta)
		if u != "" {
			if !strings.Contains(u, "://") {
				u = "https://" + u
			}
			pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr(u), 0, 0, SW_SHOWNORMAL)
			toast("Abriendo " + urlHost(u) + "…")
		}
		return
	case "hotkey":
		if sendHotkey(b.Keys) {
			toast(b.Keys)
		}
		return
	case "text":
		sendText(b.Text)
		return
	case "media":
		runMedia(b.Accion)
		return
	case "page":
		if p := b.Page - 1; p >= 0 && p < numPages() {
			page = p
			pageSound()
			startAnim()
		}
		return
	}
	t := slotID(bp, i)
	switch b.Accion {
	case "taskManager":
		keyCombo(0x11, 0x10, 0x1B)
	case "calculator":
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("calc.exe"), 0, 0, SW_SHOWNORMAL)
	case "explorer":
		pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("explorer.exe"), 0, 0, SW_SHOWNORMAL)
	case "settings":
		openSettings()
	case "desktop":
		keyCombo(0x5B, 0x44)
	case "clipboard":
		keyCombo(0x5B, 0x56)
	case "emoji":
		keyCombo(0x5B, 0xBE)
	case "empty":
		if !runningHold {
			openPicker(bp, i)
		}
	case "screenshot":
		r, _, _ := pShellExecuteW.Call(0, wstrPtr("open"), wstrPtr("ms-screenclip:"), 0, 0, SW_SHOWNORMAL)
		if r <= 32 {
			keyCombo(0x5B, 0x10, 0x53) // Win + Shift + S
		}
	case "folder":
		dir := cfg.Carpeta
		if dir == "" {
			dir = knownFolder(FOLDERID_Downloads)
		}
		if dir != "" {
			shellOpen(dir)
		}
	case "closeApp":
		if !(pendingTile == t && time.Now().Before(pendingUntil)) {
			closeTarget = lastFg
			if !closableWindow(closeTarget) {
				flash(t, "Nada que cerrar")
				return
			}
		}
		if needsConfirm(t, true) {
			return
		}
		if closableWindow(closeTarget) {
			pPostMessageW.Call(closeTarget, 0x0010, 0, 0) // WM_CLOSE
		}
		closeTarget = 0
	case "sleep":
		if needsConfirm(t, cfg.ConfirmarLuna) {
			return
		}
		if strings.EqualFold(cfg.AccionLuna, "screenOff") {
			pPostMessageW.Call(0xFFFF, WM_SYSCOMMAND, SC_MONITORPOWER, 2)
		} else if !demo {
			enableShutdownPrivilege()
			pSetSuspendState.Call(0, 0, 0)
		}
	case "shutdown":
		if needsConfirm(t, cfg.ConfirmarApagar) {
			return
		}
		if !demo {
			cmd := exec.Command("shutdown", "/s", "/t", "0")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			_ = cmd.Start()
		}
	case "lock":
		if needsConfirm(t, true) {
			return
		}
		if !demo {
			pLockWorkStation.Call()
		}
	}
	// llegó hasta acá = la acción se ejecutó
	if msg := actionFeedback(b.Accion); msg != "" {
		toast(msg)
	}
}

func actionFeedback(k string) string {
	switch k {
	case "taskManager":
		return "Abriendo el Administrador de tareas…"
	case "calculator":
		return "Abriendo la calculadora…"
	case "explorer":
		return "Abriendo el Explorador…"
	case "desktop":
		return "Mostrando el escritorio"
	case "clipboard":
		return "Historial del portapapeles"
	case "emoji":
		return "Panel de emojis"
	case "screenshot":
		return "Captura de pantalla"
	case "folder":
		if cfg.Carpeta != "" {
			return "Abriendo " + filepathBase(cfg.Carpeta) + "…"
		}
		return "Abriendo Descargas…"
	case "closeApp":
		return "Cerrando la ventana"
	case "sleep":
		if strings.EqualFold(cfg.AccionLuna, "screenOff") {
			return "Apagando la pantalla…"
		}
		return "Suspendiendo…"
	case "shutdown":
		return "Apagando el PC…"
	case "lock":
		return "Bloqueando…"
	}
	return ""
}

// ---------------------------------------------------------------- apps

type AppEntry struct {
	Name string
	Ruta string
}

type iconData struct {
	w, h int
	pix  []byte // RGBA no premultiplicado
	col  uint32
	img  *Image
	fail bool
}

var (
	appsMu     sync.Mutex
	appsList   []AppEntry
	appsReady  bool
	iconCache  = map[string]*iconData{}
	iconWanted = map[string]bool{}
	iconReq    = make(chan string, 4096)
)

const WM_APP_REFRESH = WM_APP + 1

func appIcon(ruta string) (*Image, uint32) {
	appsMu.Lock()
	d := iconCache[ruta]
	if d == nil && !iconWanted[ruta] {
		iconWanted[ruta] = true
		select {
		case iconReq <- ruta:
		default:
			iconWanted[ruta] = false
		}
	}
	appsMu.Unlock()
	if d == nil || d.fail {
		return nil, 0xFF34373C
	}
	if d.img == nil && d.pix != nil {
		d.img = newImage(d.w, d.h, d.pix)
	}
	return d.img, d.col
}

func appsWorker() {
	runtime.LockOSThread()
	defer func() { recover() }()
	pCoInitializeEx.Call(0, 2)
	// Primero los íconos ya configurados (para la página 2).
	drainIcons(false)
	list := enumAppsFolder()
	if len(list) == 0 {
		list = enumStartMenu()
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	appsMu.Lock()
	appsList, appsReady = list, true
	appsMu.Unlock()
	pPostMessageW.Call(hwnd, WM_APP_REFRESH, 0, 0)
	drainIcons(true)
}

func drainIcons(forever bool) {
	for {
		var ruta string
		if forever {
			ruta = <-iconReq
		} else {
			select {
			case ruta = <-iconReq:
			default:
				return
			}
		}
		d := loadIcon(ruta)
		appsMu.Lock()
		iconCache[ruta] = d
		appsMu.Unlock()
		if len(iconReq) == 0 || len(iconReq)%8 == 0 {
			pPostMessageW.Call(hwnd, WM_APP_REFRESH, 0, 0)
		}
	}
}

func shellItemName(item uintptr, sigdn uint32) string {
	var p uintptr
	if comCall(item, 5, uintptr(sigdn), uintptr(unsafe.Pointer(&p))) != 0 || p == 0 {
		return ""
	}
	s := utf16PtrToString(p)
	pCoTaskMemFree.Call(p)
	return s
}

func skipAppName(n string) bool {
	l := strings.ToLower(n)
	for _, w := range []string{"uninstall", "desinstal", "readme", "léame", "leame", "release notes", "documentation", "documentación"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return n == ""
}

func enumAppsFolder() []AppEntry {
	var folder uintptr
	r, _, _ := pSHGetKnownFolderItem.Call(uintptr(unsafe.Pointer(&FOLDERID_AppsFolder)), 0, 0,
		uintptr(unsafe.Pointer(&IID_IShellItem)), uintptr(unsafe.Pointer(&folder)))
	if r != 0 || folder == 0 {
		return nil
	}
	defer comRelease(folder)
	var enum uintptr
	if comCall(folder, 3, 0, uintptr(unsafe.Pointer(&BHID_EnumItems)), uintptr(unsafe.Pointer(&IID_IEnumShellItems)), uintptr(unsafe.Pointer(&enum))) != 0 || enum == 0 {
		return nil
	}
	defer comRelease(enum)
	seen := map[string]bool{}
	var out []AppEntry
	for i := 0; i < 3000; i++ {
		var item uintptr
		var got uint32
		if comCall(enum, 3, 1, uintptr(unsafe.Pointer(&item)), uintptr(unsafe.Pointer(&got))) != 0 || got == 0 || item == 0 {
			break
		}
		name := shellItemName(item, 0)        // SIGDN_NORMALDISPLAY
		id := shellItemName(item, 0x80018001) // SIGDN_PARENTRELATIVEPARSING (AUMID)
		comRelease(item)
		if skipAppName(name) || id == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, AppEntry{Name: name, Ruta: `shell:AppsFolder\` + id})
	}
	return out
}

// enumStartMenu es el plan B: accesos directos del menú Inicio.
func enumStartMenu() []AppEntry {
	dirs := []string{
		filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
	}
	seen := map[string]bool{}
	var out []AppEntry
	for _, d := range dirs {
		filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".lnk" && ext != ".url" {
				return nil
			}
			name := strings.TrimSuffix(info.Name(), filepath.Ext(p))
			if skipAppName(name) || seen[strings.ToLower(name)] {
				return nil
			}
			seen[strings.ToLower(name)] = true
			out = append(out, AppEntry{Name: name, Ruta: p})
			return nil
		})
	}
	return out
}

var (
	pSHParseDisplayName = shell32x.NewProc("SHParseDisplayName")
	pSHGetFileInfoW     = shell32x.NewProc("SHGetFileInfoW")
	pSHGetImageList     = shell32x.NewProc("SHGetImageList")
	pGetIconInfo        = user32.NewProc("GetIconInfo")
	pDestroyIcon        = user32.NewProc("DestroyIcon")
	IID_IImageList      = parseGUID("46EB5926-582E-4017-9FDF-E8998DAA0950")
)

func loadIcon(ruta string) *iconData {
	w, h, buf := iconViaFactory(ruta)
	if w < 16 || h < 16 || blank(buf) {
		w, h, buf = iconViaImageList(ruta)
	}
	if w < 16 || h < 16 || blank(buf) {
		return &iconData{fail: true}
	}
	return processIcon(w, h, buf)
}

func blank(buf []byte) bool {
	n := 0
	for i := 0; i+3 < len(buf); i += 4 {
		if buf[i+3] != 0 || buf[i] != 0 || buf[i+1] != 0 || buf[i+2] != 0 {
			n++
		}
	}
	return n < len(buf)/4/50
}

func iconViaFactory(ruta string) (int, int, []byte) {
	var item uintptr
	r, _, _ := pSHCreateItemFromParsingName.Call(wstrPtr(ruta), 0, uintptr(unsafe.Pointer(&IID_IShellItem)), uintptr(unsafe.Pointer(&item)))
	if r != 0 || item == 0 {
		return 0, 0, nil
	}
	defer comRelease(item)
	var fac uintptr
	if comCall(item, 0, uintptr(unsafe.Pointer(&IID_IShellItemImageFactory)), uintptr(unsafe.Pointer(&fac))) != 0 || fac == 0 {
		return 0, 0, nil
	}
	defer comRelease(fac)
	const sz = 128
	var hbmp uintptr
	size := uintptr(sz) | uintptr(sz)<<32                                                 // SIZE{cx, cy} por valor
	if comCall(fac, 3, size, 0x4|0x1, uintptr(unsafe.Pointer(&hbmp))) != 0 || hbmp == 0 { // ICONONLY|BIGGERSIZEOK
		return 0, 0, nil
	}
	defer pDeleteObject.Call(hbmp)
	return bitmapBGRA(hbmp)
}

func iconViaImageList(ruta string) (int, int, []byte) {
	var pidl uintptr
	r, _, _ := pSHParseDisplayName.Call(wstrPtr(ruta), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if r != 0 || pidl == 0 {
		return 0, 0, nil
	}
	defer pCoTaskMemFree.Call(pidl)
	type shfileinfo struct {
		Icon        uintptr
		IconIndex   int32
		Attributes  uint32
		DisplayName [260]uint16
		TypeName    [80]uint16
	}
	var sfi shfileinfo
	pSHGetFileInfoW.Call(pidl, 0, uintptr(unsafe.Pointer(&sfi)), unsafe.Sizeof(sfi), 0x8|0x4000) // PIDL|SYSICONINDEX
	for _, list := range []uintptr{4, 2} {                                                       // JUMBO, EXTRALARGE
		var il uintptr
		if r, _, _ := pSHGetImageList.Call(list, uintptr(unsafe.Pointer(&IID_IImageList)), uintptr(unsafe.Pointer(&il))); r != 0 || il == 0 {
			continue
		}
		var hicon uintptr
		comCall(il, 10, uintptr(sfi.IconIndex), 1, uintptr(unsafe.Pointer(&hicon))) // GetIcon(ILD_TRANSPARENT)
		comRelease(il)
		if hicon == 0 {
			continue
		}
		type iconInfo struct {
			FIcon       int32
			XHot, YHot  uint32
			Mask, Color uintptr
		}
		var ii iconInfo
		pGetIconInfo.Call(hicon, uintptr(unsafe.Pointer(&ii)))
		var w, h int
		var buf []byte
		if ii.Color != 0 {
			w, h, buf = bitmapBGRA(ii.Color)
			pDeleteObject.Call(ii.Color)
		}
		if ii.Mask != 0 {
			pDeleteObject.Call(ii.Mask)
		}
		pDestroyIcon.Call(hicon)
		if w >= 16 && !blank(buf) {
			return w, h, buf
		}
	}
	return 0, 0, nil
}

func bitmapBGRA(hbmp uintptr) (int, int, []byte) {
	type bitmap struct {
		Type, Width, Height, WidthBytes int32
		Planes, BitsPixel               uint16
		Bits                            uintptr
	}
	var bm bitmap
	pGetObjectW.Call(hbmp, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	w, h := int(bm.Width), int(bm.Height)
	if h < 0 {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return 0, 0, nil
	}
	type bmih struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
		Colors                 [4]uint32
	}
	bi := bmih{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	buf := make([]byte, w*h*4)
	hdc, _, _ := pGetDC.Call(0)
	pGetDIBits.Call(hdc, hbmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	pReleaseDC.Call(0, hdc)
	return w, h, buf
}

// processIcon convierte BGRA (quizá premultiplicado) a RGBA, recorta íconos
// pequeños dentro de un lienzo grande y calcula un color de fondo.
func processIcon(w, h int, buf []byte) *iconData {
	premul := true
	anyAlpha := false
	for i := 0; i < w*h; i++ {
		b, g, rr, a := buf[i*4], buf[i*4+1], buf[i*4+2], buf[i*4+3]
		if a != 0 {
			anyAlpha = true
		}
		if rr > a || g > a || b > a {
			premul = false
		}
	}
	pix := make([]byte, w*h*4)
	minX, minY, maxX, maxY := w, h, -1, -1
	var sr, sg, sb, sn int
	for i := 0; i < w*h; i++ {
		b, g, rr, a := int(buf[i*4]), int(buf[i*4+1]), int(buf[i*4+2]), int(buf[i*4+3])
		if !anyAlpha {
			a = 255
		} else if premul && a > 0 {
			rr, g, b = rr*255/a, g*255/a, b*255/a
		}
		pix[i*4], pix[i*4+1], pix[i*4+2], pix[i*4+3] = byte(rr), byte(g), byte(b), byte(a)
		if a > 16 {
			x, y := i%w, i/w
			minX, minY, maxX, maxY = minI(minX, x), minI(minY, y), maxI(maxX, x), maxI(maxY, y)
		}
		if a > 200 {
			sr, sg, sb, sn = sr+rr, sg+g, sb+b, sn+1
		}
	}
	// Ícono chico en lienzo grande (p. ej. 32 px dentro de 256): recortar.
	if maxX >= 0 {
		bw, bh := maxX-minX+1, maxY-minY+1
		side := maxI(bw, bh)
		if side < w*6/10 {
			side = side + side/8
			cx, cy := (minX+maxX)/2, (minY+maxY)/2
			x0, y0 := cx-side/2, cy-side/2
			np := make([]byte, side*side*4)
			for y := 0; y < side; y++ {
				for x := 0; x < side; x++ {
					sx, sy := x0+x, y0+y
					if sx < 0 || sy < 0 || sx >= w || sy >= h {
						continue
					}
					copy(np[(y*side+x)*4:(y*side+x)*4+4], pix[(sy*w+sx)*4:(sy*w+sx)*4+4])
				}
			}
			pix, w, h = np, side, side
		}
	}
	col := uint32(0xFF34373C)
	if sn > 0 {
		r := float32(sr/sn)*0.38 + 22
		g := float32(sg/sn)*0.38 + 22
		b := float32(sb/sn)*0.38 + 24
		col = 0xFF000000 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
	}
	return &iconData{w: w, h: h, pix: pix, col: col}
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}
