//go:build windows

package main

// Páginas automáticas: cuando una app queda en primer plano, el panel salta a
// la página que el usuario le asignó; al salir de esa app, vuelve a donde estaba.

import (
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

type AppPage struct {
	Exe  string `json:"exe"`  // nombre del ejecutable en minúsculas, ej. "discord.exe"
	Name string `json:"name"` // nombre para mostrar
	Page int    `json:"page"` // página del panel (1 = principal)
}

var (
	pIsWindowVisible   = user32.NewProc("IsWindowVisible")
	pGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
)

var (
	autoLastExe  string
	autoActive   bool // estamos en una página puesta automáticamente
	autoPage     int  // la página que pusimos
	autoReturnTo int  // a dónde volver al salir de la app
)

func fgExe() string {
	if lastFg == 0 {
		return ""
	}
	if ok, _, _ := pIsWindow.Call(lastFg); ok == 0 {
		return "-" // la app de adelante se cerró
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(lastFg, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == ownPid {
		return ""
	}
	return processExe(pid)
}

func ruleFor(exe string) (AppPage, bool) {
	for _, r := range cfg.AppPages {
		if strings.EqualFold(r.Exe, exe) {
			return r, true
		}
	}
	return AppPage{}, false
}

// checkAutoPage se llama desde el tick.
func checkAutoPage() {
	if len(cfg.AppPages) == 0 && !autoActive {
		return
	}
	exe := fgExe()
	if exe == "" || exe == autoLastExe {
		return
	}
	autoLastExe = exe
	// no interrumpir si el usuario está usando el panel
	busy := settingsOpen || pickerOpen || historyOpen || reorderID >= 0 || swiping || vswiping || dragging || idle
	r, ok := ruleFor(exe)
	if ok {
		p := r.Page - 1
		if p < 0 || p >= numPages() || busy {
			return
		}
		if !autoActive {
			autoReturnTo = page
		}
		autoActive, autoPage = true, p
		if page != p {
			page = p
			startAnim()
		}
		return
	}
	if autoActive {
		autoActive = false
		// volver solo si el usuario no se movió de la página automática
		if page == autoPage && !busy && autoReturnTo < numPages() {
			page = autoReturnTo
			startAnim()
		}
	}
}

// ---------------------------------------------------------------- apps abiertas

type openApp struct{ Exe, Name string }

var openAppsTmp map[string]string

var enumOpenAppsCB = syscall.NewCallback(func(w, _ uintptr) uintptr {
	if v, _, _ := pIsWindowVisible.Call(w); v == 0 {
		return 1
	}
	if windowText(w) == "" || !closableWindow(w) {
		return 1
	}
	ex, _, _ := pGetWindowLongPtrW.Call(w, ^uintptr(19)) // GWL_EXSTYLE = -20
	if ex&WS_EX_TOOLWINDOW != 0 {
		return 1
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(w, uintptr(unsafe.Pointer(&pid)))
	if pid == ownPid {
		return 1
	}
	exe := processExe(pid)
	switch exe {
	case "", "explorer.exe", "applicationframehost.exe", "textinputhost.exe", "systemsettings.exe", "shellexperiencehost.exe", "searchhost.exe", "startmenuexperiencehost.exe":
		return 1
	}
	if _, ok := openAppsTmp[exe]; !ok {
		openAppsTmp[exe] = friendlyName(exe)
	}
	return 1
})

// listOpenApps devuelve las apps con ventana abierta (una por ejecutable).
func listOpenApps() []openApp {
	openAppsTmp = map[string]string{}
	pEnumWindows.Call(enumOpenAppsCB, 0)
	var out []openApp
	for exe, name := range openAppsTmp {
		if _, used := ruleFor(exe); used {
			continue
		}
		out = append(out, openApp{exe, name})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func addAppPage(a openApp, p int) {
	cfg.AppPages = append(cfg.AppPages, AppPage{Exe: a.Exe, Name: a.Name, Page: p})
	saveConfig(cfg)
	autoLastExe = "" // aplicar de inmediato si esa app está al frente
}

func removeAppPage(i int) {
	if i < 0 || i >= len(cfg.AppPages) {
		return
	}
	cfg.AppPages = append(cfg.AppPages[:i], cfg.AppPages[i+1:]...)
	saveConfig(cfg)
}

// pageName: nombre corto de una página del panel (1-based).
func pageName(p int) string {
	switch p - 1 {
	case 0:
		return "Principal"
	case 1:
		return "Botones"
	case pageMonitor:
		return "Monitor"
	case pageMixer:
		return "Mezclador"
	}
	return "Página " + itoa(p)
}
