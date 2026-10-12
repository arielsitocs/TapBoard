//go:build windows

package main

import (
	"strings"
	"unsafe"
)

const (
	eRender         = 0
	eCapture        = 1
	eConsole        = 0
	eMultimedia     = 1
	eCommunications = 2

	DEVICE_STATE_ACTIVE = 1
	CLSCTX_ALL          = 23
	STGM_READ           = 0
)

var (
	CLSID_MMDeviceEnumerator = parseGUID("BCDE0395-E52F-467C-8E3D-C4579291692E")
	IID_IMMDeviceEnumerator  = parseGUID("A95664D2-9614-4F35-A746-DE8DB63617E6")
	IID_IAudioEndpointVolume = parseGUID("5CDF2C82-841E-4546-9722-0CF74078229A")
	CLSID_PolicyConfigClient = parseGUID("870AF99C-171D-4F9E-AF0D-E63DF40C2BC9")
	IID_IPolicyConfig        = parseGUID("F8679F50-850A-41CF-9C72-430F290290C8")
	PKEY_Device_FriendlyName = propertyKey{parseGUID("A45C254E-DF1C-4EFD-8020-67D146A850E0"), 14}
)

type propertyKey struct {
	Fmtid GUID
	Pid   uint32
}

type propVariant struct {
	Vt         uint16
	R1, R2, R3 uint16
	Val        uintptr
	Pad        uintptr
}

// Índices de vtable
const (
	// IMMDeviceEnumerator
	enumEnumAudioEndpoints      = 3
	enumGetDefaultAudioEndpoint = 4
	enumGetDevice               = 5
	// IMMDeviceCollection
	collGetCount = 3
	collItem     = 4
	// IMMDevice
	devActivate          = 3
	devOpenPropertyStore = 4
	devGetId             = 5
	// IPropertyStore
	psGetValue = 5
	// IAudioEndpointVolume
	volSetMasterScalar = 7
	volGetMasterScalar = 9
	volSetMute         = 14
	volGetMute         = 15
	// IPolicyConfig
	pcSetDefaultEndpoint = 13
)

type AudioDevice struct {
	ID   string
	Name string
}

type AudioState struct {
	OK         bool
	Volume     float32
	Muted      bool
	MicOK      bool
	MicMuted   bool
	DeviceID   string
	DeviceName string
}

type Audio struct {
	enum uintptr
}

func newAudio() *Audio {
	a := &Audio{}
	pCoCreateInstance.Call(uintptr(unsafe.Pointer(&CLSID_MMDeviceEnumerator)), 0, CLSCTX_ALL,
		uintptr(unsafe.Pointer(&IID_IMMDeviceEnumerator)), uintptr(unsafe.Pointer(&a.enum)))
	return a
}

func (a *Audio) defaultDevice(flow, role uintptr) uintptr {
	if a.enum == 0 {
		return 0
	}
	var dev uintptr
	if comCall(a.enum, enumGetDefaultAudioEndpoint, flow, role, uintptr(unsafe.Pointer(&dev))) != 0 {
		return 0
	}
	return dev
}

func endpointVolume(dev uintptr) uintptr {
	var vol uintptr
	if comCall(dev, devActivate, uintptr(unsafe.Pointer(&IID_IAudioEndpointVolume)), CLSCTX_ALL, 0,
		uintptr(unsafe.Pointer(&vol))) != 0 {
		return 0
	}
	return vol
}

func deviceID(dev uintptr) string {
	var p uintptr
	if comCall(dev, devGetId, uintptr(unsafe.Pointer(&p))) != 0 || p == 0 {
		return ""
	}
	s := utf16PtrToString(p)
	pCoTaskMemFree.Call(p)
	return s
}

func deviceName(dev uintptr) string {
	var store uintptr
	if comCall(dev, devOpenPropertyStore, STGM_READ, uintptr(unsafe.Pointer(&store))) != 0 || store == 0 {
		return ""
	}
	defer comRelease(store)
	var pv propVariant
	if comCall(store, psGetValue, uintptr(unsafe.Pointer(&PKEY_Device_FriendlyName)), uintptr(unsafe.Pointer(&pv))) != 0 {
		return ""
	}
	s := ""
	if pv.Vt == 31 { // VT_LPWSTR
		s = utf16PtrToString(pv.Val)
	}
	pPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	return s
}

// ShortName extrae el texto entre paréntesis: "Altavoces (ATK N9 PRO)" -> "ATK N9 PRO".
func ShortName(name string) string {
	i := strings.Index(name, "(")
	j := strings.LastIndex(name, ")")
	if i >= 0 && j > i+1 {
		return strings.Join(strings.Fields(name[i+1:j]), " ")
	}
	return strings.Join(strings.Fields(name), " ")
}

func (a *Audio) State() AudioState {
	var st AudioState
	dev := a.defaultDevice(eRender, eConsole)
	if dev != 0 {
		st.DeviceID = deviceID(dev)
		st.DeviceName = deviceName(dev)
		if vol := endpointVolume(dev); vol != 0 {
			var v float32
			var m int32
			comCall(vol, volGetMasterScalar, uintptr(unsafe.Pointer(&v)))
			comCall(vol, volGetMute, uintptr(unsafe.Pointer(&m)))
			st.Volume, st.Muted, st.OK = v, m != 0, true
			comRelease(vol)
		}
		comRelease(dev)
	}
	mic := a.defaultDevice(eCapture, eConsole)
	if mic != 0 {
		if vol := endpointVolume(mic); vol != 0 {
			var m int32
			comCall(vol, volGetMute, uintptr(unsafe.Pointer(&m)))
			st.MicMuted, st.MicOK = m != 0, true
			comRelease(vol)
		}
		comRelease(mic)
	}
	return st
}

func (a *Audio) SetVolume(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	dev := a.defaultDevice(eRender, eConsole)
	if dev == 0 {
		return
	}
	if vol := endpointVolume(dev); vol != 0 {
		comCall(vol, volSetMasterScalar, f32(v), 0)
		if v > 0 {
			comCall(vol, volSetMute, 0, 0)
		}
		comRelease(vol)
	}
	comRelease(dev)
}

func setMute(dev uintptr, mute bool) {
	if vol := endpointVolume(dev); vol != 0 {
		m := uintptr(0)
		if mute {
			m = 1
		}
		comCall(vol, volSetMute, m, 0)
		comRelease(vol)
	}
}

func (a *Audio) ToggleMute() {
	st := a.State()
	dev := a.defaultDevice(eRender, eConsole)
	if dev == 0 {
		return
	}
	setMute(dev, !st.Muted)
	comRelease(dev)
}

func (a *Audio) ToggleMic() {
	st := a.State()
	target := !st.MicMuted
	// Silenciar tanto el micrófono por defecto como el de comunicaciones.
	seen := map[string]bool{}
	for _, role := range []uintptr{eConsole, eCommunications} {
		dev := a.defaultDevice(eCapture, role)
		if dev == 0 {
			continue
		}
		id := deviceID(dev)
		if !seen[id] {
			seen[id] = true
			setMute(dev, target)
		}
		comRelease(dev)
	}
}

func (a *Audio) RenderDevices() []AudioDevice {
	var out []AudioDevice
	if a.enum == 0 {
		return out
	}
	var coll uintptr
	if comCall(a.enum, enumEnumAudioEndpoints, eRender, DEVICE_STATE_ACTIVE, uintptr(unsafe.Pointer(&coll))) != 0 || coll == 0 {
		return out
	}
	defer comRelease(coll)
	var n uint32
	comCall(coll, collGetCount, uintptr(unsafe.Pointer(&n)))
	for i := uint32(0); i < n; i++ {
		var dev uintptr
		if comCall(coll, collItem, uintptr(i), uintptr(unsafe.Pointer(&dev))) != 0 || dev == 0 {
			continue
		}
		out = append(out, AudioDevice{ID: deviceID(dev), Name: deviceName(dev)})
		comRelease(dev)
	}
	return out
}

func setDefaultDevice(id string) bool {
	var pc uintptr
	r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&CLSID_PolicyConfigClient)), 0, CLSCTX_ALL,
		uintptr(unsafe.Pointer(&IID_IPolicyConfig)), uintptr(unsafe.Pointer(&pc)))
	if r != 0 || pc == 0 {
		return false
	}
	defer comRelease(pc)
	idp := wstrPtr(id)
	ok := true
	for _, role := range []uintptr{eConsole, eMultimedia, eCommunications} {
		if comCall(pc, pcSetDefaultEndpoint, idp, role) != 0 {
			ok = false
		}
	}
	return ok
}

// NextDevice cambia la salida de audio a la siguiente de la lista.
// filtros: partes del nombre de los dispositivos a alternar (vacío = todos).
// Devuelve los filtros que no coincidieron con ningún dispositivo.
func (a *Audio) NextDevice(filtros []string) (faltan []string) {
	all := a.RenderDevices()
	var list []AudioDevice
	used := map[string]bool{}
	if len(filtros) == 0 {
		list = all
	} else {
		for _, f := range filtros {
			f = strings.ToLower(strings.Join(strings.Fields(f), " "))
			if f == "" {
				continue
			}
			best, score := -1, 0
			for i, d := range all {
				if used[d.ID] {
					continue
				}
				n := strings.ToLower(strings.Join(strings.Fields(d.Name), " "))
				sn := strings.ToLower(ShortName(d.Name))
				sc := 0
				switch {
				case sn == f || n == f:
					sc = 3
				case strings.HasPrefix(sn, f):
					sc = 2
				case strings.Contains(n, f):
					sc = 1
				}
				if sc > score {
					best, score = i, sc
				}
			}
			if best < 0 {
				faltan = append(faltan, f)
				continue
			}
			used[all[best].ID] = true
			list = append(list, all[best])
		}
	}
	if len(list) == 0 {
		return faltan
	}
	cur := a.State().DeviceID
	next := 0
	for i, d := range list {
		if d.ID == cur {
			next = (i + 1) % len(list)
			break
		}
	}
	setDefaultDevice(list[next].ID)
	return faltan
}
