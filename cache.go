//go:build windows

package main

// Caché de dibujos: lo que no cambia (tarjetas, sombras, íconos escalados,
// fondo) se dibuja una vez en una imagen y después solo se copia.

import "sort"

type rcEntry struct {
	sn   *Snapshot
	last int
}

var (
	rcache  = map[string]*rcEntry{}
	frameNo int
)

const rcacheMax = 260

func cachedRender(key string, w, h int, draw func(c *Canvas)) *Snapshot {
	if e, ok := rcache[key]; ok {
		e.last = frameNo
		return e.sn
	}
	if w <= 0 || h <= 0 {
		return nil
	}
	sn, c := newSnapshot(w, h)
	if sn == nil {
		return nil
	}
	draw(c)
	c.Close()
	rcache[key] = &rcEntry{sn: sn, last: frameNo}
	return sn
}

// pruneCache libera lo que lleva más tiempo sin usarse.
func pruneCache() {
	if len(rcache) <= rcacheMax {
		return
	}
	type kv struct {
		k string
		l int
	}
	var all []kv
	for k, e := range rcache {
		all = append(all, kv{k, e.last})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].l < all[j].l })
	for _, x := range all[:len(all)-rcacheMax*3/4] {
		rcache[x.k].sn.Free()
		delete(rcache, x.k)
	}
}

func clearRenderCache() {
	for k, e := range rcache {
		e.sn.Free()
		delete(rcache, k)
	}
}

func ri(f float32) int { return int(f + 0.5) }

func hex32(v uint32) string {
	const d = "0123456789abcdef"
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = d[v&15]
		v >>= 4
	}
	return string(b)
}
