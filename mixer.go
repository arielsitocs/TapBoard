//go:build windows

package main

// Página 4: volumen por aplicación (mezclador).

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	IID_IAudioSessionManager2  = parseGUID("77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F")
	IID_IAudioSessionControl2  = parseGUID("BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D")
	IID_ISimpleAudioVolume     = parseGUID("87CE5498-68D6-44E5-9215-6DA47EF883D8")
	IID_IAudioMeterInformation = parseGUID("C02216F6-8C67-4B5B-9D00-D008E73E0064")
)

type mixApp struct {
	key    string
	name   string
	exe    string // ruta completa (para el ícono)
	system bool
	vols   []uintptr // ISimpleAudioVolume de cada sesión del grupo
	meters []uintptr
	volume float32
	muted  bool
	peak   float32
	active bool
}

var (
	mixApps   []*mixApp
	mixLast   time.Time
	mixDrag   = -1
	mixSmooth = map[string]float32{}
)

const maxMixApps = 6

func processPath(pid uint32) string {
	h, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 520)
	n := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

var friendlyNames = map[string]string{
	"chrome": "Chrome", "msedge": "Edge", "firefox": "Firefox", "zen": "Zen", "opera": "Opera", "brave": "Brave",
	"discord": "Discord", "spotify": "Spotify", "steam": "Steam", "steamwebhelper": "Steam",
	"whatsapp": "WhatsApp", "whatsapp.root": "WhatsApp", "obs64": "OBS", "vlc": "VLC", "teams": "Teams", "ms-teams": "Teams",
	"zoom": "Zoom", "wow": "World of Warcraft", "projectzomboid64": "Project Zomboid", "ts3sims4": "Los Sims 4",
}

func friendlyName(exe string) string {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe)))
	if n, ok := friendlyNames[base]; ok {
		return n
	}
	if base == "" {
		return "App"
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

func releaseMix() {
	for _, a := range mixApps {
		for _, v := range a.vols {
			comRelease(v)
		}
		for _, m := range a.meters {
			comRelease(m)
		}
	}
	mixApps = nil
}

// refreshMixer vuelve a leer las sesiones de audio del dispositivo actual.
func refreshMixer() {
	if demo {
		demoMixer()
		return
	}
	dev := audio.defaultDevice(eRender, eConsole)
	if dev == 0 {
		releaseMix()
		return
	}
	defer comRelease(dev)
	var mgr uintptr
	if comCall(dev, devActivate, uintptr(unsafe.Pointer(&IID_IAudioSessionManager2)), CLSCTX_ALL, 0, uintptr(unsafe.Pointer(&mgr))) != 0 || mgr == 0 {
		return
	}
	defer comRelease(mgr)
	var enum uintptr
	if comCall(mgr, 5, uintptr(unsafe.Pointer(&enum))) != 0 || enum == 0 {
		return
	}
	defer comRelease(enum)
	var n int32
	comCall(enum, 3, uintptr(unsafe.Pointer(&n)))
	groups := map[string]*mixApp{}
	var order []*mixApp
	for i := int32(0); i < n; i++ {
		var ctl uintptr
		if comCall(enum, 4, uintptr(i), uintptr(unsafe.Pointer(&ctl))) != 0 || ctl == 0 {
			continue
		}
		var ctl2 uintptr
		comCall(ctl, 0, uintptr(unsafe.Pointer(&IID_IAudioSessionControl2)), uintptr(unsafe.Pointer(&ctl2)))
		var state int32
		comCall(ctl, 3, uintptr(unsafe.Pointer(&state)))
		if ctl2 == 0 || state == 2 { // expirada
			if ctl2 != 0 {
				comRelease(ctl2)
			}
			comRelease(ctl)
			continue
		}
		var pid uint32
		comCall(ctl2, 14, uintptr(unsafe.Pointer(&pid)))
		system := comCall(ctl2, 15) == 0
		key, name, exe := "", "", ""
		if system {
			key, name = "__system", "Sonidos del sistema"
		} else {
			exe = processPath(pid)
			if exe == "" {
				comRelease(ctl2)
				comRelease(ctl)
				continue
			}
			key = strings.ToLower(filepath.Base(exe))
			name = friendlyName(exe)
		}
		var vol, meter uintptr
		comCall(ctl, 0, uintptr(unsafe.Pointer(&IID_ISimpleAudioVolume)), uintptr(unsafe.Pointer(&vol)))
		comCall(ctl, 0, uintptr(unsafe.Pointer(&IID_IAudioMeterInformation)), uintptr(unsafe.Pointer(&meter)))
		comRelease(ctl2)
		comRelease(ctl)
		if vol == 0 {
			if meter != 0 {
				comRelease(meter)
			}
			continue
		}
		g := groups[key]
		if g == nil {
			g = &mixApp{key: key, name: name, exe: exe, system: system, muted: true}
			groups[key] = g
			order = append(order, g)
			var v float32
			comCall(vol, 4, uintptr(unsafe.Pointer(&v)))
			g.volume = v
		}
		var m int32
		comCall(vol, 6, uintptr(unsafe.Pointer(&m)))
		if m == 0 {
			g.muted = false
		}
		if state == 1 {
			g.active = true
		}
		g.vols = append(g.vols, vol)
		if meter != 0 {
			g.meters = append(g.meters, meter)
		}
	}
	// El sistema al final; activas primero; luego por nombre.
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.system != b.system {
			return !a.system
		}
		if a.active != b.active {
			return a.active
		}
		return a.name < b.name
	})
	// Mientras se arrastra, conservar el valor que pone el dedo.
	if mixDrag >= 0 && mixDrag < len(mixApps) {
		for _, g := range order {
			if g.key == mixApps[mixDrag].key {
				g.volume = mixApps[mixDrag].volume
			}
		}
	}
	releaseMix()
	mixApps = order
	updatePeaks()
}

func updatePeaks() {
	for _, a := range mixApps {
		p := float32(0)
		for _, m := range a.meters {
			var v float32
			if comCall(m, 3, uintptr(unsafe.Pointer(&v))) == 0 && v > p {
				p = v
			}
		}
		// caída suave del medidor
		old := mixSmooth[a.key]
		if p < old {
			p = old*0.8 + p*0.2
		}
		mixSmooth[a.key] = p
		a.peak = p
	}
}

func setAppVolume(i int, v float32) {
	if i < 0 || i >= len(mixApps) {
		return
	}
	v = float32(math.Max(0, math.Min(1, float64(v))))
	a := mixApps[i]
	a.volume = v
	for _, h := range a.vols {
		comCall(h, 3, f32(v), 0)
		if v > 0 && a.muted {
			comCall(h, 5, 0, 0)
		}
	}
	if v > 0 {
		a.muted = false
	}
	invalidate(hwnd)
}

func toggleAppMute(i int) {
	if i < 0 || i >= len(mixApps) {
		return
	}
	a := mixApps[i]
	a.muted = !a.muted
	m := uintptr(0)
	if a.muted {
		m = 1
	}
	for _, h := range a.vols {
		comCall(h, 5, m, 0)
	}
	if a.muted {
		toast(a.name + " silenciado")
	} else {
		toast(a.name + " con sonido")
	}
}

// ---- geometría: tarjetas 2×3

func mixCardRect(i int) RectF {
	cols, rows := 2, 3
	bottom := pad*1.2 + 10
	cw := (panelW - pad*float32(cols+1)) / float32(cols)
	ch := (panelH - pad*float32(rows) - bottom) / float32(rows)
	return RectF{pad + float32(i%cols)*(cw+pad), pad + float32(i/cols)*(ch+pad), cw, ch}
}

func mixIconRect(i int) RectF {
	r := mixCardRect(i)
	s := r.H * 0.52
	return RectF{r.X + r.H*0.2, r.Y + (r.H-s)/2, s, s}
}

func mixSliderRange(i int) (x0, x1, y float32) {
	r := mixCardRect(i)
	ic := mixIconRect(i)
	x0 = ic.X + ic.W + r.H*0.2
	x1 = r.X + r.W - r.H*0.62
	y = r.Y + r.H*0.64
	return
}

// mixerRect: 2000+i = tarjeta (barra), 2100+i = ícono (silenciar).
func mixerRect(id int) RectF {
	if id >= 2100 {
		return mixIconRect(id - 2100)
	}
	return mixCardRect(id - 2000)
}

func mixerHit(x, y int32) int {
	fx, fy := float32(x), float32(y)
	for i := 0; i < len(mixApps) && i < maxMixApps; i++ {
		ic := mixIconRect(i)
		if inRect(RectF{ic.X - ic.W*0.15, ic.Y - ic.H*0.15, ic.W * 1.3, ic.H * 1.3}, fx, fy) {
			return 2100 + i
		}
		r := mixCardRect(i)
		x0, x1, y := mixSliderRange(i)
		if inRect(RectF{x0 - r.H*0.12, y - r.H*0.22, x1 - x0 + r.H*0.24, r.H * 0.44}, fx, fy) {
			return 2000 + i
		}
	}
	return -1
}

func mixerSetFromX(i int, x int32) {
	x0, x1, _ := mixSliderRange(i)
	setAppVolume(i, (float32(x)-x0)/(x1-x0))
}

func mixerActivate(id int) {
	if id >= 2100 {
		toggleAppMute(id - 2100)
	}
}

func paintMixerPage(c *Canvas) {
	if len(mixApps) == 0 {
		c.Text("Ninguna app está usando el audio ahora.", textFont, panelH*0.04, FontStyleRegular, colGrey,
			RectF{0, 0, panelW, panelH}, AlignCenter, AlignCenter)
		return
	}
	for i, a := range mixApps {
		if i >= maxMixApps {
			break
		}
		r := mixCardRect(i)
		bg := themeTile(0xFF26282C)
		if pressed == 2000+i || mixDrag == i {
			bg = lighten(bg, 0.05)
		}
		flatCard(c, r.X, r.Y, r.W, r.H, min32(r.W, r.H)*0.2, bg)
		// ícono (tocar = silenciar)
		ic := mixIconRect(i)
		bgc := uint32(0xFF33363B)
		if a.muted {
			bgc = colRedOn
		}
		if pressed == 2100+i {
			bgc = lighten(bgc, 0.15)
		}
		c.RoundRect(ic.X, ic.Y, ic.W, ic.H, ic.W*0.24, bgc)
		var img *Image
		if a.system {
			img = icons["speaker_high_volume"]
		} else {
			img, _ = appIcon(a.exe)
		}
		is := ic.W * 0.66
		if img != nil {
			c.DrawImage(img, ic.X+(ic.W-is)/2, ic.Y+(ic.H-is)/2, is, is)
		} else {
			c.Text(strings.ToUpper(a.name[:1]), semiFont, ic.H*0.45, semiStyle, colWhite, ic, AlignCenter, AlignCenter)
		}
		if a.muted {
			c.Line(ic.X+ic.W*0.2, ic.Y+ic.H*0.2, ic.X+ic.W*0.8, ic.Y+ic.H*0.8, ic.W*0.07, 0xFFFF5A5A)
		}
		// nombre y porcentaje
		x0, x1, y := mixSliderRange(i)
		fs := r.H * 0.15
		c.Text(a.name, semiFont, fs, semiStyle, colWhite, RectF{x0, r.Y + r.H*0.14, r.X + r.W - x0 - r.H*0.15, fs * 1.6}, AlignNear, AlignCenter)
		pct := fmt.Sprintf("%d %%", int(math.Round(float64(a.volume)*100)))
		if a.muted {
			pct = "Mudo"
		}
		c.Text(pct, semiFont, fs*0.85, semiStyle, colGrey, RectF{x1 + r.H*0.04, y - fs, r.X + r.W - x1 - r.H*0.16, fs * 2}, AlignFar, AlignCenter)
		// barra
		th := r.H * 0.07
		c.RoundRect(x0, y-th/2, x1-x0, th, th/2, 0xFF3A3D42)
		fill := uint32(colBlue)
		if a.muted {
			fill = colDim
		}
		fx := x0 + (x1-x0)*a.volume
		if fx-x0 > 1 {
			if a.muted {
				c.RoundRect(x0, y-th/2, fx-x0, th, th/2, fill)
			} else {
				c.RoundRectGradH(x0, y-th/2, fx-x0, th, th/2, 0xFF1E88E5, 0xFF4FC3F7)
			}
		}
		// medidor de nivel (lo que suena ahora)
		if a.peak > 0.005 && !a.muted {
			mw := (x1 - x0) * a.volume * float32(math.Min(1, float64(a.peak)*1.4))
			c.RoundRect(x0, y+th*0.9, mw, th*0.35, th*0.17, 0xFF66BB6A)
		}
		kr := r.H * 0.08
		if mixDrag == i {
			kr *= 1.15
		}
		c.Circle(fx, y+kr*0.06, kr*1.04, 0x40000000)
		c.Circle(fx, y, kr, colWhite)
	}
}

func demoMixer() {
	if len(mixApps) > 0 {
		for _, a := range mixApps {
			if !a.muted {
				a.peak = float32(0.3 + 0.3*math.Sin(float64(time.Now().UnixMilli())/300+float64(len(a.name))))
			}
		}
		return
	}
	mixApps = []*mixApp{
		{key: "game", name: "Project Zomboid", exe: `C:\windows\notepad.exe`, volume: 0.62, active: true},
		{key: "discord", name: "Discord", volume: 0.85, active: true},
		{key: "spotify", name: "Spotify", volume: 0.4, active: true},
		{key: "chrome", name: "Chrome", volume: 1, muted: true},
		{key: "__system", name: "Sonidos del sistema", system: true, volume: 0.5},
	}
}
