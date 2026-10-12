//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	gdiplus = syscall.NewLazyDLL("gdiplus.dll")

	gGdiplusStartup           = gdiplus.NewProc("GdiplusStartup")
	gCreateFromHDC            = gdiplus.NewProc("GdipCreateFromHDC")
	gDeleteGraphics           = gdiplus.NewProc("GdipDeleteGraphics")
	gSetSmoothingMode         = gdiplus.NewProc("GdipSetSmoothingMode")
	gSetTextRenderingHint     = gdiplus.NewProc("GdipSetTextRenderingHint")
	gSetPixelOffsetMode       = gdiplus.NewProc("GdipSetPixelOffsetMode")
	gGraphicsClear            = gdiplus.NewProc("GdipGraphicsClear")
	gCreateSolidFill          = gdiplus.NewProc("GdipCreateSolidFill")
	gDeleteBrush              = gdiplus.NewProc("GdipDeleteBrush")
	gCreatePath               = gdiplus.NewProc("GdipCreatePath")
	gDeletePath               = gdiplus.NewProc("GdipDeletePath")
	gAddPathArc               = gdiplus.NewProc("GdipAddPathArc")
	gClosePathFigure          = gdiplus.NewProc("GdipClosePathFigure")
	gFillPath                 = gdiplus.NewProc("GdipFillPath")
	gFillEllipse              = gdiplus.NewProc("GdipFillEllipse")
	gCreateFontFamilyFromName = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	gDeleteFontFamily         = gdiplus.NewProc("GdipDeleteFontFamily")
	gCreateFont               = gdiplus.NewProc("GdipCreateFont")
	gDeleteFont               = gdiplus.NewProc("GdipDeleteFont")
	gCreateStringFormat       = gdiplus.NewProc("GdipCreateStringFormat")
	gDeleteStringFormat       = gdiplus.NewProc("GdipDeleteStringFormat")
	gSetStringFormatAlign     = gdiplus.NewProc("GdipSetStringFormatAlign")
	gSetStringFormatLineAlign = gdiplus.NewProc("GdipSetStringFormatLineAlign")
	gSetStringFormatFlags     = gdiplus.NewProc("GdipSetStringFormatFlags")
	gSetStringFormatTrimming  = gdiplus.NewProc("GdipSetStringFormatTrimming")
	gDrawString               = gdiplus.NewProc("GdipDrawString")
	gGetGenericSansSerif      = gdiplus.NewProc("GdipGetGenericFontFamilySansSerif")
)

type RectF struct{ X, Y, W, H float32 }

const (
	FontStyleRegular = 0
	FontStyleBold    = 1
	UnitPixel        = 2

	AlignNear   = 0
	AlignCenter = 1
	AlignFar    = 2
)

func gdiplusInit() bool {
	type startupInput struct {
		Version                  uint32
		DebugEventCallback       uintptr
		SuppressBackgroundThread int32
		SuppressExternalCodecs   int32
	}
	in := startupInput{Version: 1}
	var token uintptr
	r, _, _ := gGdiplusStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&in)), 0)
	return r == 0
}

// Canvas envuelve un objeto Graphics de GDI+.
type Canvas struct {
	g     uintptr
	fonts map[string]uintptr // familia -> handle
}

func newCanvas(hdc uintptr) *Canvas {
	c := &Canvas{fonts: map[string]uintptr{}}
	gCreateFromHDC.Call(hdc, uintptr(unsafe.Pointer(&c.g)))
	gSetSmoothingMode.Call(c.g, 4)     // AntiAlias
	gSetPixelOffsetMode.Call(c.g, 4)   // HighQuality
	gSetTextRenderingHint.Call(c.g, 4) // AntiAlias
	return c
}

func (c *Canvas) Close() {
	gDeleteGraphics.Call(c.g) // familias, fuentes y formatos quedan en caché global
}

// Cachés globales: crear fuentes en GDI+ es caro, así que se reutilizan.
var (
	famCache  = map[string]uintptr{}
	fontCache = map[fontKey]uintptr{}
	fmtCache  = map[uint64]uintptr{}
)

type fontKey struct {
	fam   uintptr
	size  int32 // en décimas de píxel
	style int
}

func (c *Canvas) Clear(argb uint32) { gGraphicsClear.Call(c.g, uintptr(argb)) }

// canvasAlpha multiplica la opacidad de todo lo que se rellena (para fundidos).
var canvasAlpha float32 = 1

func (c *Canvas) SetAlpha(a float32) { canvasAlpha = a }

func applyAlpha(argb uint32) uint32 {
	if canvasAlpha >= 1 {
		return argb
	}
	a := float32(argb>>24) * canvasAlpha
	return uint32(a)<<24 | argb&0x00FFFFFF
}

func brush(argb uint32) uintptr {
	argb = applyAlpha(argb)
	var b uintptr
	gCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&b)))
	return b
}

func roundPath(x, y, w, h, r float32) uintptr {
	if r*2 > w {
		r = w / 2
	}
	if r*2 > h {
		r = h / 2
	}
	var path uintptr
	gCreatePath.Call(0, uintptr(unsafe.Pointer(&path)))
	d := r * 2
	if d < 1 {
		d = 1
	}
	gAddPathArc.Call(path, f32(x), f32(y), f32(d), f32(d), f32(180), f32(90))
	gAddPathArc.Call(path, f32(x+w-d), f32(y), f32(d), f32(d), f32(270), f32(90))
	gAddPathArc.Call(path, f32(x+w-d), f32(y+h-d), f32(d), f32(d), f32(0), f32(90))
	gAddPathArc.Call(path, f32(x), f32(y+h-d), f32(d), f32(d), f32(90), f32(90))
	gClosePathFigure.Call(path)
	return path
}

func (c *Canvas) RoundRect(x, y, w, h, r float32, argb uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	b := brush(argb)
	gFillPath.Call(c.g, b, path)
	gDeleteBrush.Call(b)
	gDeletePath.Call(path)
}

// RoundRectGrad rellena con un degradado vertical (top -> bottom).
func (c *Canvas) RoundRectGrad(x, y, w, h, r float32, top, bottom uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	rc := RectF{x, y - 1, w, h + 2}
	top, bottom = applyAlpha(top), applyAlpha(bottom)
	var b uintptr
	gCreateLineBrushFromRect.Call(uintptr(unsafe.Pointer(&rc)), uintptr(top), uintptr(bottom), 1, 3, uintptr(unsafe.Pointer(&b)))
	if b == 0 {
		b = brush(bottom)
	}
	gFillPath.Call(c.g, b, path)
	gDeleteBrush.Call(b)
	gDeletePath.Call(path)
}

// ClipRoundRect limita el dibujo a un rectángulo redondeado (ResetClip para quitarlo).
func (c *Canvas) ClipRoundRect(x, y, w, h, r float32) {
	path := roundPath(x, y, w, h, r)
	gSetClipPath.Call(c.g, path, 0)
	gDeletePath.Call(path)
}

func (c *Canvas) ResetClip() { gResetClip.Call(c.g) }

func (c *Canvas) Translate(dx, dy float32) {
	gResetWorldTransform.Call(c.g)
	gTranslateWorldTransform.Call(c.g, f32(dx), f32(dy), 0)
}

func (c *Canvas) ResetTransform() { gResetWorldTransform.Call(c.g) }

func (c *Canvas) Circle(cx, cy, r float32, argb uint32) {
	b := brush(argb)
	gFillEllipse.Call(c.g, b, f32(cx-r), f32(cy-r), f32(2*r), f32(2*r))
	gDeleteBrush.Call(b)
}

func (c *Canvas) family(name string) uintptr {
	if f, ok := famCache[name]; ok {
		return f
	}
	var f uintptr
	gCreateFontFamilyFromName.Call(wstrPtr(name), 0, uintptr(unsafe.Pointer(&f)))
	famCache[name] = f
	return f
}

func cachedFont(fam uintptr, size float32, style int) uintptr {
	k := fontKey{fam, int32(size*10 + 0.5), style}
	if f, ok := fontCache[k]; ok {
		return f
	}
	if len(fontCache) > 400 {
		for kk, f := range fontCache {
			gDeleteFont.Call(f)
			delete(fontCache, kk)
		}
	}
	var font uintptr
	gCreateFont.Call(fam, f32(float32(k.size)/10), uintptr(style), UnitPixel, uintptr(unsafe.Pointer(&font)))
	if font != 0 {
		fontCache[k] = font
	}
	return font
}

func cachedFormat(flags uintptr, h, v int) uintptr {
	k := uint64(flags)<<8 | uint64(h)<<4 | uint64(v)
	if f, ok := fmtCache[k]; ok {
		return f
	}
	var fmtH uintptr
	gCreateStringFormat.Call(flags, 0, uintptr(unsafe.Pointer(&fmtH)))
	gSetStringFormatAlign.Call(fmtH, uintptr(h))
	gSetStringFormatLineAlign.Call(fmtH, uintptr(v))
	gSetStringFormatTrimming.Call(fmtH, 3) // EllipsisCharacter
	fmtCache[k] = fmtH
	return fmtH
}

// Text dibuja texto dentro de un rectángulo con la alineación indicada.
func (c *Canvas) Text(s, familyName string, sizePx float32, style int, argb uint32, rc RectF, hAlign, vAlign int) {
	c.text(s, familyName, sizePx, style, argb, rc, hAlign, vAlign, 0x1000) // NoWrap
}

// TextWrap igual que Text, pero permite varias líneas.
func (c *Canvas) TextWrap(s, familyName string, sizePx float32, style int, argb uint32, rc RectF, hAlign, vAlign int) {
	c.text(s, familyName, sizePx, style, argb, rc, hAlign, vAlign, 0x2000) // LineLimit
}

func (c *Canvas) text(s, familyName string, sizePx float32, style int, argb uint32, rc RectF, hAlign, vAlign int, flags uintptr) {
	if s == "" || sizePx <= 0 || noText {
		return
	}
	fam := c.family(familyName)
	if fam == 0 {
		fam = c.family("Segoe UI")
	}
	if fam == 0 {
		fam = c.family("Arial")
	}
	if fam == 0 {
		if f, ok := famCache["__generic"]; ok {
			fam = f
		} else {
			gGetGenericSansSerif.Call(uintptr(unsafe.Pointer(&fam)))
			famCache["__generic"] = fam
		}
	}
	if fam == 0 {
		return
	}
	font := cachedFont(fam, sizePx, style)
	if font == 0 {
		return
	}
	fmtH := cachedFormat(flags, hAlign, vAlign)
	b := brush(argb)
	gDrawString.Call(c.g, wstrPtr(s), ^uintptr(0), font, uintptr(unsafe.Pointer(&rc)), fmtH, b)
	gDeleteBrush.Call(b)
}

// fontExists indica si una familia tipográfica está instalada.
func fontExists(name string) bool {
	var f uintptr
	r, _, _ := gCreateFontFamilyFromName.Call(wstrPtr(name), 0, uintptr(unsafe.Pointer(&f)))
	if r == 0 && f != 0 {
		gDeleteFontFamily.Call(f)
		return true
	}
	return false
}

var (
	gCreateBitmapFromScan0 = gdiplus.NewProc("GdipCreateBitmapFromScan0")
	gDrawImageRect         = gdiplus.NewProc("GdipDrawImageRect")
	gSetInterpolationMode  = gdiplus.NewProc("GdipSetInterpolationMode")
	gCreatePen1            = gdiplus.NewProc("GdipCreatePen1")
	gDeletePen             = gdiplus.NewProc("GdipDeletePen")
	gSetPenStartCap        = gdiplus.NewProc("GdipSetPenStartCap")
	gSetPenEndCap          = gdiplus.NewProc("GdipSetPenEndCap")
	gDrawLine              = gdiplus.NewProc("GdipDrawLine")

	gCreateLineBrushFromRect = gdiplus.NewProc("GdipCreateLineBrushFromRect")
	gSetClipPath             = gdiplus.NewProc("GdipSetClipPath")
	gResetClip               = gdiplus.NewProc("GdipResetClip")
	gTranslateWorldTransform = gdiplus.NewProc("GdipTranslateWorldTransform")
	gResetWorldTransform     = gdiplus.NewProc("GdipResetWorldTransform")
)

// Image es un bitmap de GDI+ creado a partir de un PNG embebido.
type Image struct {
	mono bool // ícono plano (glifo blanco): se dibuja un poco más chico
	id   int
	bmp  uintptr
	buf  []byte // se mantiene vivo mientras exista el bitmap
	W    int
	H    int
}

// newImage crea un bitmap GDI+ desde píxeles RGBA (no premultiplicados).
func newImage(w, h int, rgba []byte) *Image {
	buf := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		r, g, b, a := uint32(rgba[i*4]), uint32(rgba[i*4+1]), uint32(rgba[i*4+2]), uint32(rgba[i*4+3])
		// formato PARGB en memoria: B, G, R, A premultiplicado
		buf[i*4+0] = byte(b * a / 255)
		buf[i*4+1] = byte(g * a / 255)
		buf[i*4+2] = byte(r * a / 255)
		buf[i*4+3] = byte(a)
	}
	imageSeq++
	im := &Image{id: imageSeq, buf: buf, W: w, H: h}
	const PixelFormat32bppPARGB = 0x000E200B
	gCreateBitmapFromScan0.Call(uintptr(w), uintptr(h), uintptr(w*4), PixelFormat32bppPARGB,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&im.bmp)))
	return im
}

var imageSeq int

// DrawImage dibuja una imagen escalada. El escalado de alta calidad se hace
// una sola vez por tamaño y se guarda en caché (dibujar después es muy rápido).
func (c *Canvas) DrawImage(im *Image, x, y, w, h float32) {
	if im == nil || im.bmp == 0 || w < 1 || h < 1 {
		return
	}
	iw, ih := int(w+0.5), int(h+0.5)
	if iw > 1600 || ih > 1600 {
		c.drawImageHQ(im, x, y, w, h)
		return
	}
	key := "img|" + itoa(im.id) + "|" + itoa(iw) + "|" + itoa(ih)
	sn := cachedRender(key, iw, ih, func(cc *Canvas) { cc.drawImageHQ(im, 0, 0, float32(iw), float32(ih)) })
	if sn == nil {
		c.drawImageHQ(im, x, y, w, h)
		return
	}
	c.DrawSnapshot(sn, int(x+0.5), int(y+0.5))
}

func (c *Canvas) drawImageHQ(im *Image, x, y, w, h float32) {
	gSetInterpolationMode.Call(c.g, 7) // HighQualityBicubic
	gDrawImageRect.Call(c.g, im.bmp, f32(x), f32(y), f32(w), f32(h))
}

func (c *Canvas) Line(x0, y0, x1, y1, width float32, argb uint32) {
	var pen uintptr
	gCreatePen1.Call(uintptr(argb), f32(width), 0, uintptr(unsafe.Pointer(&pen)))
	if pen == 0 {
		return
	}
	gSetPenStartCap.Call(pen, 2) // Round
	gSetPenEndCap.Call(pen, 2)
	gDrawLine.Call(c.g, pen, f32(x0), f32(y0), f32(x1), f32(y1))
	gDeletePen.Call(pen)
}

var (
	gFillPolygon = gdiplus.NewProc("GdipFillPolygon")
	gDrawLines   = gdiplus.NewProc("GdipDrawLines")
)

type PointF struct{ X, Y float32 }

func (c *Canvas) FillPoly(pts []PointF, argb uint32) {
	if len(pts) < 3 {
		return
	}
	b := brush(argb)
	gFillPolygon.Call(c.g, b, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), 0)
	gDeleteBrush.Call(b)
}

func (c *Canvas) Polyline(pts []PointF, width float32, argb uint32) {
	if len(pts) < 2 {
		return
	}
	var pen uintptr
	gCreatePen1.Call(uintptr(argb), f32(width), 0, uintptr(unsafe.Pointer(&pen)))
	if pen == 0 {
		return
	}
	gSetPenStartCap.Call(pen, 2)
	gSetPenEndCap.Call(pen, 2)
	gSetPenLineJoin.Call(pen, 2) // Round
	gDrawLines.Call(c.g, pen, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	gDeletePen.Call(pen)
}

var gSetPenLineJoin = gdiplus.NewProc("GdipSetPenLineJoin")

var (
	gDrawArc  = gdiplus.NewProc("GdipDrawArc")
	gDrawPath = gdiplus.NewProc("GdipDrawPath")
)

func (c *Canvas) Ellipse(cx, cy, rx, ry float32, argb uint32) {
	b := brush(argb)
	gFillEllipse.Call(c.g, b, f32(cx-rx), f32(cy-ry), f32(2*rx), f32(2*ry))
	gDeleteBrush.Call(b)
}

func newPen(argb uint32, width float32) uintptr {
	var pen uintptr
	gCreatePen1.Call(uintptr(applyAlpha(argb)), f32(width), 0, uintptr(unsafe.Pointer(&pen)))
	return pen
}

// Arc dibuja un arco (grados, 0 = derecha, sentido horario) con puntas redondas.
func (c *Canvas) Arc(cx, cy, r, start, sweep, width float32, argb uint32) {
	if sweep == 0 {
		return
	}
	pen := newPen(argb, width)
	if pen == 0 {
		return
	}
	gSetPenStartCap.Call(pen, 2)
	gSetPenEndCap.Call(pen, 2)
	gDrawArc.Call(c.g, pen, f32(cx-r), f32(cy-r), f32(2*r), f32(2*r), f32(start), f32(sweep))
	gDeletePen.Call(pen)
}

// RoundRectStroke dibuja solo el borde de un rectángulo redondeado.
func (c *Canvas) RoundRectStroke(x, y, w, h, r, width float32, argb uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	pen := newPen(argb, width)
	if pen != 0 {
		gDrawPath.Call(c.g, pen, path)
		gDeletePen.Call(pen)
	}
	gDeletePath.Call(path)
}

// RoundRectGradH rellena con un degradado horizontal (izquierda -> derecha).
func (c *Canvas) RoundRectGradH(x, y, w, h, r float32, left, right uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	rc := RectF{x - 1, y, w + 2, h}
	var b uintptr
	gCreateLineBrushFromRect.Call(uintptr(unsafe.Pointer(&rc)), uintptr(applyAlpha(left)), uintptr(applyAlpha(right)), 0, 3, uintptr(unsafe.Pointer(&b)))
	if b == 0 {
		b = brush(right)
	}
	gFillPath.Call(c.g, b, path)
	gDeleteBrush.Call(b)
	gDeletePath.Call(path)
}

// ---- instantáneas de página (para deslizar rápido)

var (
	gGetImageGraphicsContext = gdiplus.NewProc("GdipGetImageGraphicsContext")
	gDisposeImage            = gdiplus.NewProc("GdipDisposeImage")
	gDrawImageRectI          = gdiplus.NewProc("GdipDrawImageRectI")
	gSetCompositingQuality   = gdiplus.NewProc("GdipSetCompositingQuality")
)

type Snapshot struct {
	bmp  uintptr
	w, h int
}

// newSnapshot crea un lienzo transparente del tamaño dado.
func newSnapshot(w, h int) (*Snapshot, *Canvas) {
	sn := &Snapshot{w: w, h: h}
	gCreateBitmapFromScan0.Call(uintptr(w), uintptr(h), 0, 0x000E200B, 0, uintptr(unsafe.Pointer(&sn.bmp)))
	if sn.bmp == 0 {
		return nil, nil
	}
	c := &Canvas{}
	gGetImageGraphicsContext.Call(sn.bmp, uintptr(unsafe.Pointer(&c.g)))
	gSetSmoothingMode.Call(c.g, 4)
	gSetPixelOffsetMode.Call(c.g, 4)
	gSetTextRenderingHint.Call(c.g, 4)
	c.Clear(0) // la memoria nueva puede traer basura: partir transparente
	return sn, c
}

func (s *Snapshot) Free() {
	if s != nil && s.bmp != 0 {
		gDisposeImage.Call(s.bmp)
		s.bmp = 0
	}
}

// DrawSnapshotRect dibuja una instantánea escalada (bilineal, para animaciones).
func (c *Canvas) DrawSnapshotRect(s *Snapshot, x, y, w, h float32) {
	if s == nil || s.bmp == 0 {
		return
	}
	gSetInterpolationMode.Call(c.g, 3) // Bilinear
	gSetCompositingQuality.Call(c.g, 1)
	gDrawImageRect.Call(c.g, s.bmp, f32(x), f32(y), f32(w), f32(h))
	gSetCompositingQuality.Call(c.g, 0)
}

// DrawSnapshot copia la instantánea sin escalar (rápido).
func (c *Canvas) DrawSnapshot(s *Snapshot, x, y int) {
	if s == nil || s.bmp == 0 {
		return
	}
	gSetInterpolationMode.Call(c.g, 5)  // NearestNeighbor
	gSetPixelOffsetMode.Call(c.g, 3)    // None
	gSetCompositingQuality.Call(c.g, 1) // HighSpeed
	gDrawImageRectI.Call(c.g, s.bmp, uintptr(int32(x)), uintptr(int32(y)), uintptr(s.w), uintptr(s.h))
	gSetPixelOffsetMode.Call(c.g, 4)
	gSetCompositingQuality.Call(c.g, 0)
}

// RoundRectGradMode: degradado con modo (0 horizontal, 1 vertical, 2 diagonal, 3 diagonal inversa).
func (c *Canvas) RoundRectGradMode(x, y, w, h, r float32, from, to uint32, mode int) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	rc := RectF{x - 1, y - 1, w + 2, h + 2}
	var b uintptr
	gCreateLineBrushFromRect.Call(uintptr(unsafe.Pointer(&rc)), uintptr(applyAlpha(from)), uintptr(applyAlpha(to)), uintptr(mode), 3, uintptr(unsafe.Pointer(&b)))
	if b == 0 {
		b = brush(to)
	}
	gFillPath.Call(c.g, b, path)
	gDeleteBrush.Call(b)
	gDeletePath.Call(path)
}

var gCreatePen2 = gdiplus.NewProc("GdipCreatePen2")

// RoundRectStrokeGrad: borde con degradado vertical.
func (c *Canvas) RoundRectStrokeGrad(x, y, w, h, r, width float32, top, bottom uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	path := roundPath(x, y, w, h, r)
	rc := RectF{x, y - 1, w, h + 2}
	var b uintptr
	gCreateLineBrushFromRect.Call(uintptr(unsafe.Pointer(&rc)), uintptr(applyAlpha(top)), uintptr(applyAlpha(bottom)), 1, 3, uintptr(unsafe.Pointer(&b)))
	if b != 0 {
		var pen uintptr
		gCreatePen2.Call(b, f32(width), 2, uintptr(unsafe.Pointer(&pen)))
		if pen != 0 {
			gDrawPath.Call(c.g, pen, path)
			gDeletePen.Call(pen)
		}
		gDeleteBrush.Call(b)
	}
	gDeletePath.Call(path)
}

var noText = os.Getenv("PANEL_NOTEXT") != ""
