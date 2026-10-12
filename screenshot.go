//go:build windows

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"
	"unsafe"
)

// captureMonitorOf toma una captura del monitor donde está la ventana dada
// y la guarda como PNG en Imágenes\Capturas de pantalla.
func captureMonitorOf(hwnd uintptr) (string, error) {
	mon, _, _ := pMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTOPRIMARY)
	var mi MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	r := mi.RcMonitor
	w, h := int(r.R-r.L), int(r.B-r.T)
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("monitor inválido")
	}

	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBitmap.Call(screen, uintptr(w), uintptr(h))
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	pBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(r.L), uintptr(r.T), SRCCOPY|CAPTUREBLT)
	pSelectObject.Call(mem, old)

	type bitmapInfoHeader struct {
		Size          uint32
		Width         int32
		Height        int32
		Planes        uint16
		BitCount      uint16
		Compression   uint32
		SizeImage     uint32
		XPelsPerMeter int32
		YPelsPerMeter int32
		ClrUsed       uint32
		ClrImportant  uint32
	}
	type bitmapInfo struct {
		Header bitmapInfoHeader
		Colors [4]uint32
	}
	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // de arriba hacia abajo
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	buf := make([]byte, w*h*4)
	pGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = buf[i*4+2]
		img.Pix[i*4+1] = buf[i*4+1]
		img.Pix[i*4+2] = buf[i*4+0]
		img.Pix[i*4+3] = 255
	}

	dir := knownFolder(FOLDERID_Pictures)
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Pictures")
	}
	dir = filepath.Join(dir, "Screenshots")
	_ = os.MkdirAll(dir, 0o755)
	name := filepath.Join(dir, "Panel_"+time.Now().Format("2006-01-02_15-04-05")+".png")
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return name, nil
}
