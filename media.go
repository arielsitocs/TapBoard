//go:build windows

package main

// Lo que suena en Windows (Spotify, YouTube, navegador, etc.) a través de
// GlobalSystemMediaTransportControlsSessionManager (WinRT, vía COM crudo).

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	combase                             = syscall.NewLazyDLL("combase.dll")
	shcore                              = syscall.NewLazyDLL("shcore.dll")
	pRoInitialize                       = combase.NewProc("RoInitialize")
	pRoGetActivationFactory             = combase.NewProc("RoGetActivationFactory")
	pWindowsCreateString                = combase.NewProc("WindowsCreateString")
	pWindowsDeleteString                = combase.NewProc("WindowsDeleteString")
	pWindowsGetStringRawBuffer          = combase.NewProc("WindowsGetStringRawBuffer")
	pCreateStreamOverRandomAccessStream = shcore.NewProc("CreateStreamOverRandomAccessStream")
	pGetSystemTimeAsFileTime            = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimeAsFileTime")

	IID_GSMTCManagerStatics = parseGUID("2050C4EE-11A0-57DE-AED7-C97C70338245")
	IID_IAsyncInfo          = parseGUID("00000036-0000-0000-C000-000000000046")
	IID_IStream             = parseGUID("0000000C-0000-0000-C000-000000000046")
)

const (
	mediaPlaying = 4
	mediaPaused  = 5
)

type MediaState struct {
	OK       bool
	App      string
	Title    string
	Artist   string
	Status   int32
	Pos      int64 // 100 ns
	End      int64 // 100 ns
	Updated  int64 // FILETIME del momento en que se midió Pos
	ThumbVer int
	ThumbW   int
	ThumbH   int
	ThumbPix []byte // RGBA
}

var (
	mediaMu  sync.Mutex
	mediaCur MediaState
)

func getMedia() MediaState {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	return mediaCur
}

func nowFiletime() int64 {
	var ft int64
	pGetSystemTimeAsFileTime.Call(uintptr(unsafe.Pointer(&ft)))
	return ft
}

func hstring(s string) uintptr {
	u, _ := syscall.UTF16FromString(s)
	var h uintptr
	pWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h
}

func hstrToString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := pWindowsGetStringRawBuffer.Call(h, uintptr(unsafe.Pointer(&n)))
	s := ""
	if p != 0 && n > 0 {
		s = syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
	}
	pWindowsDeleteString.Call(h)
	return s
}

// getHString llama a un getter que devuelve un HSTRING.
func getHString(obj uintptr, idx int) string {
	var h uintptr
	if comCall(obj, idx, uintptr(unsafe.Pointer(&h))) != 0 {
		return ""
	}
	return hstrToString(h)
}

// await espera una IAsyncOperation y devuelve su resultado (puntero COM).
func await(op uintptr, timeout time.Duration) uintptr {
	if op == 0 {
		return 0
	}
	defer comRelease(op)
	var info uintptr
	if comCall(op, 0, uintptr(unsafe.Pointer(&IID_IAsyncInfo)), uintptr(unsafe.Pointer(&info))) != 0 || info == 0 {
		return 0
	}
	defer comRelease(info)
	deadline := time.Now().Add(timeout)
	for {
		var st int32
		comCall(info, 7, uintptr(unsafe.Pointer(&st)))
		if st == 1 {
			break
		}
		if st >= 2 || time.Now().After(deadline) {
			return 0
		}
		time.Sleep(8 * time.Millisecond)
	}
	var res uintptr
	if comCall(op, 8, uintptr(unsafe.Pointer(&res))) != 0 {
		return 0
	}
	return res
}

func mediaWorker() {
	runtime.LockOSThread()
	defer func() { recover() }()
	if pRoInitialize.Find() != nil {
		return
	}
	pRoInitialize.Call(1) // multihilo
	cls := hstring("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager")
	var statics uintptr
	r, _, _ := pRoGetActivationFactory.Call(cls, uintptr(unsafe.Pointer(&IID_GSMTCManagerStatics)), uintptr(unsafe.Pointer(&statics)))
	pWindowsDeleteString.Call(cls)
	if r != 0 || statics == 0 {
		return
	}
	var op uintptr
	comCall(statics, 6, uintptr(unsafe.Pointer(&op))) // RequestAsync
	mgr := await(op, 5*time.Second)
	comRelease(statics)
	if mgr == 0 {
		return
	}
	lastKey := ""
	thumbVer := 0
	var thumbW, thumbH int
	var thumbPix []byte
	for {
		st := MediaState{}
		var sess uintptr
		comCall(mgr, 6, uintptr(unsafe.Pointer(&sess))) // GetCurrentSession
		if sess != 0 {
			st.OK = true
			st.App = getHString(sess, 6)
			var info uintptr
			if comCall(sess, 9, uintptr(unsafe.Pointer(&info))) == 0 && info != 0 {
				comCall(info, 7, uintptr(unsafe.Pointer(&st.Status)))
				comRelease(info)
			}
			var tl uintptr
			if comCall(sess, 8, uintptr(unsafe.Pointer(&tl))) == 0 && tl != 0 {
				var start int64
				comCall(tl, 6, uintptr(unsafe.Pointer(&start)))
				comCall(tl, 7, uintptr(unsafe.Pointer(&st.End)))
				comCall(tl, 10, uintptr(unsafe.Pointer(&st.Pos)))
				comCall(tl, 11, uintptr(unsafe.Pointer(&st.Updated)))
				st.End -= start
				st.Pos -= start
				comRelease(tl)
			}
			var pop uintptr
			comCall(sess, 7, uintptr(unsafe.Pointer(&pop))) // TryGetMediaPropertiesAsync
			if props := await(pop, 2*time.Second); props != 0 {
				st.Title = getHString(props, 6)
				st.Artist = getHString(props, 9)
				if st.Artist == "" {
					st.Artist = getHString(props, 8) // AlbumArtist
				}
				key := st.App + "|" + st.Title + "|" + st.Artist
				if key != lastKey {
					lastKey = key
					thumbW, thumbH, thumbPix = 0, 0, nil
					var ref uintptr
					if comCall(props, 15, uintptr(unsafe.Pointer(&ref))) == 0 && ref != 0 {
						if w, h, pix := readThumbnail(ref); pix != nil {
							thumbW, thumbH, thumbPix = w, h, pix
						}
						comRelease(ref)
					}
					thumbVer++
				}
				comRelease(props)
			}
			comRelease(sess)
		}
		st.ThumbVer, st.ThumbW, st.ThumbH, st.ThumbPix = thumbVer, thumbW, thumbH, thumbPix
		mediaMu.Lock()
		mediaCur = st
		mediaMu.Unlock()
		select {
		case pos := <-seekCh:
			doSeek(mgr, pos)
		case <-time.After(700 * time.Millisecond):
		}
	}
}

var seekCh = make(chan int64, 4)

// doSeek mueve la canción a pos (100 ns desde el inicio) en la sesión actual.
func doSeek(mgr uintptr, pos int64) {
	var sess uintptr
	comCall(mgr, 6, uintptr(unsafe.Pointer(&sess)))
	if sess == 0 {
		return
	}
	defer comRelease(sess)
	var start int64
	var tl uintptr
	if comCall(sess, 8, uintptr(unsafe.Pointer(&tl))) == 0 && tl != 0 {
		comCall(tl, 6, uintptr(unsafe.Pointer(&start)))
		comRelease(tl)
	}
	var op uintptr
	if comCall(sess, 24, uintptr(start+pos), uintptr(unsafe.Pointer(&op))) == 0 { // TryChangePlaybackPositionAsync
		await(op, 2*time.Second)
	}
}

func readThumbnail(ref uintptr) (int, int, []byte) {
	var op uintptr
	if comCall(ref, 6, uintptr(unsafe.Pointer(&op))) != 0 { // OpenReadAsync
		return 0, 0, nil
	}
	ras := await(op, 3*time.Second)
	if ras == 0 {
		return 0, 0, nil
	}
	defer comRelease(ras)
	var stream uintptr
	r, _, _ := pCreateStreamOverRandomAccessStream.Call(ras, uintptr(unsafe.Pointer(&IID_IStream)), uintptr(unsafe.Pointer(&stream)))
	if r != 0 || stream == 0 {
		return 0, 0, nil
	}
	defer comRelease(stream)
	var data []byte
	buf := make([]byte, 64*1024)
	for len(data) < 20<<20 {
		var n uint32
		hr := comCall(stream, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		data = append(data, buf[:n]...)
		if n == 0 || hr != 0 {
			break
		}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil
	}
	return squareRGBA(img, 240)
}

// squareRGBA recorta al cuadrado central y escala a size×size (promedio de caja).
func squareRGBA(img image.Image, size int) (int, int, []byte) {
	b := img.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	if side <= 0 {
		return 0, 0, nil
	}
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	src := image.NewNRGBA(image.Rect(0, 0, side, side))
	draw.Draw(src, src.Rect, img, image.Pt(x0, y0), draw.Src)
	if side < size {
		size = side
	}
	out := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		sy0, sy1 := y*side/size, (y+1)*side/size
		for x := 0; x < size; x++ {
			sx0, sx1 := x*side/size, (x+1)*side/size
			var r, g, bb, a, n int
			for sy := sy0; sy < sy1; sy++ {
				o := sy*src.Stride + sx0*4
				for sx := sx0; sx < sx1; sx++ {
					r += int(src.Pix[o])
					g += int(src.Pix[o+1])
					bb += int(src.Pix[o+2])
					a += int(src.Pix[o+3])
					n++
					o += 4
				}
			}
			if n == 0 {
				n = 1
			}
			i := (y*size + x) * 4
			out[i], out[i+1], out[i+2], out[i+3] = byte(r/n), byte(g/n), byte(bb/n), byte(a/n)
		}
	}
	return size, size, out
}

// ---------------------------------------------------------------- UI

var (
	seekBar    RectF // zona táctil de la barra de progreso (coords. del panel)
	seekBarX0  float32
	seekBarX1  float32
	seeking    bool
	seekFrac   float32
	media      MediaState
	mediaThumb *Image
	mediaTVer  = -1
	mediaWasSt int32
)

func updateMedia() {
	st := getMedia()
	changed := st.Title != media.Title || st.Artist != media.Artist || st.Status != media.Status ||
		st.OK != media.OK || st.ThumbVer != media.ThumbVer
	if st.ThumbVer != mediaTVer {
		mediaTVer = st.ThumbVer
		mediaThumb = nil
		if st.ThumbPix != nil {
			mediaThumb = newImage(st.ThumbW, st.ThumbH, st.ThumbPix)
		}
		buildAmbient(st.ThumbW, st.ThumbH, st.ThumbPix)
	}
	// Sincroniza el botón play/pausa con lo que reporta Windows.
	if st.OK && st.Status != mediaWasSt && (st.Status == mediaPlaying || st.Status == mediaPaused) {
		playPaused = st.Status == mediaPaused
		changed = true
	}
	mediaWasSt = st.Status
	media = st
	if changed || (st.Status == mediaPlaying && st.End > 0 && tickCount%5 == 0) {
		invalidate(hwnd)
	}
}

func mediaShowing() bool {
	return cfg.MostrarSpotify && media.OK && strings.TrimSpace(media.Title) != ""
}

func fmtTime(t int64) string {
	s := int(t / 10_000_000)
	if s < 0 {
		s = 0
	}
	m := s / 60
	s %= 60
	return itoa(m) + ":" + string(rune('0'+s/10)) + string(rune('0'+s%10))
}

// drawMediaHeader dibuja carátula, título, artista y progreso en la tarjeta de volumen.
func drawMediaHeader(c *Canvas, r RectF, rightLimit float32) {
	m := r.H * 0.12
	fs := r.H * 0.12
	art := r.H * 0.5
	ax, ay := r.X+m*0.6, r.Y+m*0.5
	// la carátula y las pistas de las barras van en la imagen en caché (volCardBG)
	tx := ax + art + m*0.6
	tw := rightLimit - tx
	songCol := uint32(colWhite)
	sub := media.Artist
	if media.Status != mediaPlaying {
		songCol = colGrey
		if sub != "" {
			sub = "En pausa · " + sub
		} else {
			sub = "En pausa"
		}
	}
	c.Text(sinEmojis(media.Title), semiFont, fs*1.0, semiStyle, songCol, RectF{tx, ay + fs*0.02, tw, fs * 1.4}, AlignNear, AlignCenter)
	subW := tw
	c.Text(sinEmojis(sub), textFont, fs*0.84, FontStyleRegular, 0xFFA8AEB6, RectF{tx, ay + fs*1.3, subW, fs * 1.25}, AlignNear, AlignCenter)
	if media.End > 0 {
		pos := media.Pos
		if media.Status == mediaPlaying && media.Updated > 0 {
			pos += nowFiletime() - media.Updated
		}
		if pos > media.End {
			pos = media.End
		}
		frac := float32(pos) / float32(media.End)
		px0 := tx
		px1 := r.X + r.W - m
		py := ay + art*0.76
		th := r.H * 0.045
		seekBarX0, seekBarX1 = px0, px1
		seekBar = RectF{px0 - fs*0.5, py - fs*0.9, px1 - px0 + fs, fs * 1.6}
		if seeking {
			frac = seekFrac
			pos = int64(float64(seekFrac) * float64(media.End))
			th *= 1.8
		}
		if seeking {
			c.RoundRect(px0, py-th/2, px1-px0, th, th/2, 0x33FFFFFF)
		}
		if frac > 0 {
			c.RoundRectGradH(px0, py-th/2, (px1-px0)*frac, th, th/2, 0xDDFFFFFF, lighten(mediaAccent, 0.35))
		}
		if seeking {
			c.Circle(px0+(px1-px0)*frac, py, th*1.6, colWhite)
		}
		// tiempos en los extremos de la barra (como en Spotify)
		tr := RectF{px0, py + th*0.8, px1 - px0, fs * 1.0}
		c.Text(fmtTime(pos), textFont, fs*0.7, FontStyleRegular, 0xFFA8AEB6, tr, AlignNear, AlignCenter)
		c.Text(fmtTime(media.End), textFont, fs*0.7, FontStyleRegular, 0xFFA8AEB6, tr, AlignFar, AlignCenter)
	}
}

// demoMedia simula algo sonando (solo para pruebas con PANEL_DEMO).
func demoMedia() {
	b, _ := iconFS.ReadFile("icons/grinning_face_3d.png")
	img, _, err := image.Decode(bytes.NewReader(b))
	st := MediaState{OK: true, App: "Spotify.exe", Title: "Canción de prueba con nombre largo", Artist: "Artista de prueba",
		Status: mediaPlaying, Pos: 83 * 10_000_000, End: 225 * 10_000_000, Updated: nowFiletime(), ThumbVer: 1}
	if err == nil {
		st.ThumbW, st.ThumbH, st.ThumbPix = squareRGBA(img, 240)
	}
	mediaMu.Lock()
	mediaCur = st
	mediaMu.Unlock()
}

func seekHit(x, y int32) bool {
	return page == 0 && !swiping && mediaShowing() && media.End > 0 && seekBar.W > 0 &&
		inRect(seekBar, float32(x), float32(y))
}

func seekFracAt(x int32) float32 {
	f := (float32(x) - seekBarX0) / (seekBarX1 - seekBarX0)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f
}

func seekCommit() {
	pos := int64(float64(seekFrac) * float64(media.End))
	select {
	case seekCh <- pos:
	default:
	}
	// Actualización optimista mientras Windows confirma.
	media.Pos, media.Updated = pos, nowFiletime()
	mediaMu.Lock()
	mediaCur.Pos, mediaCur.Updated = pos, media.Updated
	mediaMu.Unlock()
}

// mediaStatic dibuja lo que no cambia de la tarjeta de música: carátula y la
// pista de la barra de progreso (en coordenadas de la tarjeta r).
func mediaStatic(c *Canvas, r RectF) {
	m := r.H * 0.12
	art := r.H * 0.5
	ax, ay := r.X+m*0.6, r.Y+m*0.5
	if mediaThumb != nil {
		c.RoundRect(ax, ay+art*0.05, art, art, art*0.18, 0x60000000) // sombra
		c.ClipRoundRect(ax, ay, art, art, art*0.18)
		c.drawImageHQ(mediaThumb, ax, ay, art, art)
		c.ResetClip()
		c.RoundRectStroke(ax+0.5, ay+0.5, art-1, art-1, art*0.18, 1, 0x26FFFFFF)
	} else {
		c.RoundRect(ax, ay, art, art, art*0.18, 0xFF2E3036)
		is := art * 0.62
		c.drawImageHQ(icons["musical_note"], ax+(art-is)/2, ay+(art-is)/2, is, is)
	}
	if media.End > 0 {
		tx := ax + art + m*0.6
		px1 := r.X + r.W - m
		py := ay + art*0.76
		th := r.H * 0.045
		c.RoundRect(tx, py-th/2, px1-tx, th, th/2, 0x33FFFFFF)
	}
}
