//go:build windows

package main

// Sonido suave al tocar: un "tic" corto generado en memoria.

import (
	"encoding/binary"
	"math"
	"syscall"
	"unsafe"
)

var (
	pPlaySoundW = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")
	clickWav    = makePop(1500, 320, 0.009, 0.050, 0.20, 0.25)
	pageWav     = makePop(700, 220, 0.016, 0.070, 0.10, 0.05)
)

func makeClick(f1, f2, amp, dur float64) []byte {
	const rate = 44100
	n := int(rate * dur)
	data := make([]byte, n*2)
	seed := uint32(12345)
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		seed = seed*1664525 + 1013904223
		noise := (float64(seed>>16)/32768 - 1) * math.Exp(-t/0.0008)
		v := 0.55*math.Sin(2*math.Pi*f1*t)*math.Exp(-t/0.0035) +
			0.45*math.Sin(2*math.Pi*f2*t)*math.Exp(-t/0.007) +
			0.25*noise
		// pequeño fundido de entrada para evitar un "pop"
		if i < 20 {
			v *= float64(i) / 20
		}
		binary.LittleEndian.PutUint16(data[i*2:], uint16(int16(v*amp*32767)))
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	return append(h, data...)
}

// makePop: "tock" corto y redondo, como una tecla mecánica suave: un tono que
// baja rápido de f0 a f1 (cuerpo) + un pequeño golpe de ruido al inicio (clic).
func makePop(f0, f1, tau, dur, amp, noiseAmt float64) []byte {
	const rate = 44100
	n := int(rate * dur)
	data := make([]byte, n*2)
	seed := uint32(987654)
	phase := 0.0
	prevNoise := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		f := f1 + (f0-f1)*math.Exp(-t/0.006)
		phase += 2 * math.Pi * f / rate
		env := math.Exp(-t / tau)
		if i < 40 { // ataque de ~1 ms
			env *= float64(i) / 40
		}
		body := math.Sin(phase) * env
		// armónico suave para que suene más "lleno"
		body += 0.25 * math.Sin(2*phase) * math.Exp(-t/(tau*0.6))
		seed = seed*1664525 + 1013904223
		nz := float64(seed>>16)/32768 - 1
		hp := nz - prevNoise // ruido filtrado (agudo)
		prevNoise = nz
		click := hp * math.Exp(-t/0.0012) * noiseAmt
		v := (body + click) * amp
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		binary.LittleEndian.PutUint16(data[i*2:], uint16(int16(v*32767)))
	}
	return wavFile(rate, data)
}

func wavFile(rate int, data []byte) []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	return append(h, data...)
}

func playWav(w []byte) {
	if !cfg.Sonidos {
		return
	}
	// SND_MEMORY | SND_ASYNC | SND_NODEFAULT
	pPlaySoundW.Call(uintptr(unsafe.Pointer(&w[0])), 0, 0x4|0x1|0x2)
}

func clickSound() { playWav(clickWav) }
func pageSound()  { playWav(pageWav) }
