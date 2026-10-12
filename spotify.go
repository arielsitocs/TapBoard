//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32x                   = syscall.NewLazyDLL("kernel32.dll")
	pEnumWindows                = user32.NewProc("EnumWindows")
	pOpenProcess                = kernel32x.NewProc("OpenProcess")
	pQueryFullProcessImageNameW = kernel32x.NewProc("QueryFullProcessImageNameW")
)

// SpotifyState describe lo que muestra Spotify de escritorio.
type SpotifyState struct {
	Running bool
	Playing bool
	Song    string
	Artist  string
}

var (
	spotifyCands []uintptr
	exeCache     = map[uint32]string{}
)

func processExe(pid uint32) string {
	if s, ok := exeCache[pid]; ok {
		return s
	}
	h, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 520)
	n := uint32(len(buf))
	r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	s := strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:n])))
	if len(exeCache) > 500 {
		exeCache = map[uint32]string{}
	}
	exeCache[pid] = s
	return s
}

func windowText(w uintptr) string {
	buf := make([]uint16, 512)
	pGetWindowTextW.Call(w, uintptr(unsafe.Pointer(&buf[0])), 512)
	return syscall.UTF16ToString(buf)
}

var enumSpotifyCB = syscall.NewCallback(func(w, _ uintptr) uintptr {
	if !strings.HasPrefix(windowClass(w), "Chrome_WidgetWin") || windowText(w) == "" {
		return 1
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(w, uintptr(unsafe.Pointer(&pid)))
	if processExe(pid) == "spotify.exe" {
		spotifyCands = append(spotifyCands, w)
	}
	return 1
})

// spotifyTitle busca entre todas las ventanas de Spotify la que tiene la
// canción ("Artista - Canción"); si ninguna, devuelve el título que haya.
func spotifyTitle() (string, bool) {
	spotifyCands = nil
	pEnumWindows.Call(enumSpotifyCB, 0)
	if len(spotifyCands) == 0 {
		return "", false
	}
	first := ""
	for _, w := range spotifyCands {
		t := windowText(w)
		if strings.Contains(t, " - ") {
			return t, true
		}
		if first == "" || strings.HasPrefix(t, "Spotify") {
			first = t
		}
	}
	return first, true
}

var spotifyLastTitle = "no encontrado"

// pollSpotify lee el título de la ventana de Spotify: "Artista - Canción"
// mientras suena, o "Spotify ..." cuando está en pausa.
func pollSpotify(_ bool) SpotifyState {
	t, ok := spotifyTitle()
	if !ok {
		spotifyLastTitle = "no encontrado"
		return SpotifyState{}
	}
	spotifyLastTitle = t
	return parseSpotifyTitle(t)
}

func parseSpotifyTitle(t string) SpotifyState {
	st := SpotifyState{Running: true}
	if t == "" || strings.HasPrefix(t, "Spotify") || t == "Advertisement" || t == "Publicidad" {
		return st
	}
	st.Playing = true
	t = sinEmojis(t)
	if i := strings.Index(t, " - "); i > 0 {
		st.Artist, st.Song = strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+3:])
	} else {
		st.Song = t
	}
	return st
}
