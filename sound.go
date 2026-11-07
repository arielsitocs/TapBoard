//go:build windows

package main

// Sonidos al tocar y al cambiar de página, generados en memoria.

import (
	"encoding/binary"
	"math"
	"syscall"
	"unsafe"
)

var (
	pPlaySoundW = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")
	// toque: golpecito grave y rápido (~30 ms), casi sin chasquido
	clickWav = tapSound{clickHz: 2200, clickQ: 1.0, clickTau: 0.0006, clickAmt: 0.06,
		f0: 380, f1: 260, pitchTau: 0.0025, bodyTau: 0.0055,
		partial: 1.5, partialTau: 0.003, partialAmt: 0.08, dur: 0.03, gain: 0.32, drive: 1.1}.render()
	// cambio de página: más grave y largo, para que se distinga del toque
	pageWav = tapSound{clickHz: 2200, clickQ: 1.0, clickTau: 0.002, clickAmt: 0.14,
		f0: 300, f1: 150, pitchTau: 0.007, bodyTau: 0.024,
		partial: 2.0, partialTau: 0.008, partialAmt: 0.12, dur: 0.1, gain: 0.22, drive: 1.3}.render()
)

// tapSound: golpe corto generado en memoria. Un chasquido de ruido filtrado en
// banda (el brillo), un cuerpo con un tono que baja de f0 a f1 (el peso) y un
// parcial inarmónico que le da timbre de vidrio o madera.
type tapSound struct {
	clickHz, clickQ, clickTau, clickAmt float64 // chasquido inicial
	f0, f1, pitchTau, bodyTau           float64 // cuerpo
	partial, partialTau, partialAmt     float64 // parcial extra (multiplica f)
	dur, gain, drive                    float64 // largo, volumen, saturación suave
}

func (p tapSound) render() []byte {
	const rate = 44100
	n := int(rate * p.dur)
	out := make([]byte, n*2)
	// resonador de 2 polos para el ruido (pasa banda)
	w := 2 * math.Pi * p.clickHz / rate
	r := 1 - w/(2*p.clickQ)
	a1, a2 := 2*r*math.Cos(w), -r*r
	var y1, y2 float64
	seed := uint32(24680)
	ph, ph2 := 0.0, 0.0
	fade := rate * 6 / 1000 // fundido final de 6 ms
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		seed = seed*1664525 + 1013904223
		nz := float64(seed>>16)/32768 - 1
		y := nz*(1-r) + a1*y1 + a2*y2
		y2, y1 = y1, y
		click := y * math.Exp(-t/p.clickTau) * p.clickAmt * 6

		f := p.f1 + (p.f0-p.f1)*math.Exp(-t/p.pitchTau)
		ph += 2 * math.Pi * f / rate
		ph2 += 2 * math.Pi * f * p.partial / rate
		att := math.Min(1, t/0.0006) // ataque de 0,6 ms: sin "pop"
		body := math.Sin(ph) * math.Exp(-t/p.bodyTau) * att
		body += p.partialAmt * math.Sin(ph2) * math.Exp(-t/p.partialTau) * att

		v := math.Tanh((body+click)*p.drive) / math.Tanh(p.drive) * p.gain
		if j := n - i; j < fade {
			v *= float64(j) / float64(fade)
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(v*32767)))
	}
	return wavFile(rate, out)
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
