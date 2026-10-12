//go:build windows

package main

import (
	"bytes"
	"embed"
	"image"
	"image/draw"
	"image/png"
	"strings"
)

// Íconos: Microsoft Fluent Emoji (licencia MIT).
//
//go:embed icons/*.png icons_flat/*.png
var iconFS embed.FS

var (
	icons     = map[string]*Image{}
	icons3D   = map[string]*Image{}
	iconsFlat = map[string]*Image{}
)

func loadIcons() {
	loadIconDir("icons", "_3d.png", icons3D)
	loadIconDir("icons_flat", "_flat.png", iconsFlat)
	applyIconSet()
}

// applyIconSet elige los íconos según el tema: planos en "Vidrio", 3D en el resto.
func applyIconSet() {
	m := map[string]*Image{}
	for k, v := range icons3D {
		m[k] = v
	}
	if isGlass() {
		for k, v := range iconsFlat {
			m[k] = v
		}
	}
	icons = m
}

func loadIconDir(dir, suffix string, dst map[string]*Image) {
	entries, _ := iconFS.ReadDir(dir)
	for _, e := range entries {
		data, err := iconFS.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		b := img.Bounds()
		rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), suffix)
		centered := centerContent(rgba)
		dst[name] = newImage(centered.Rect.Dx(), centered.Rect.Dy(), centered.Pix)
		dst[name].mono = strings.HasPrefix(name, "fx_")
	}
}

// centerContent desplaza el dibujo para que su parte visible quede
// exactamente al centro de la imagen (algunos emojis vienen descentrados).
func centerContent(src *image.NRGBA) *image.NRGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if src.Pix[y*src.Stride+x*4+3] > 16 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < 0 {
		return src
	}
	dx := (w-1)/2 - (minX+maxX)/2
	dy := (h-1)/2 - (minY+maxY)/2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Rect.Add(image.Pt(dx, dy)), src, image.Point{}, draw.Src)
	return dst
}
