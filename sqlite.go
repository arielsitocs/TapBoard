package main

// Lector mínimo de SQLite (solo lectura, incluye el archivo WAL).
// Suficiente para leer la base de datos de notificaciones de Windows.

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strings"
)

type sqliteDB struct {
	main     []byte
	wal      map[uint32][]byte
	pageSize int
	usable   int
	nPages   uint32
}

func openSQLite(path string) (*sqliteDB, error) {
	main, err := readShared(path)
	if err != nil {
		return nil, err
	}
	if len(main) < 100 || string(main[:15]) != "SQLite format 3" {
		return nil, errors.New("no es sqlite")
	}
	ps := int(binary.BigEndian.Uint16(main[16:18]))
	if ps == 1 {
		ps = 65536
	}
	db := &sqliteDB{main: main, pageSize: ps, usable: ps - int(main[20]), wal: map[uint32][]byte{}}
	db.nPages = uint32(len(main) / ps)
	if w, err := readShared(path + "-wal"); err == nil {
		db.loadWAL(w)
	}
	return db, nil
}

func (db *sqliteDB) loadWAL(w []byte) {
	if len(w) < 32 {
		return
	}
	magic := binary.BigEndian.Uint32(w[0:4])
	if magic != 0x377f0682 && magic != 0x377f0683 {
		return
	}
	ps := int(binary.BigEndian.Uint32(w[8:12]))
	if ps != db.pageSize {
		return
	}
	salt1 := binary.BigEndian.Uint32(w[16:20])
	salt2 := binary.BigEndian.Uint32(w[20:24])
	pending := map[uint32][]byte{}
	off := 32
	for off+24+ps <= len(w) {
		h := w[off : off+24]
		if binary.BigEndian.Uint32(h[8:12]) != salt1 || binary.BigEndian.Uint32(h[12:16]) != salt2 {
			break
		}
		pg := binary.BigEndian.Uint32(h[0:4])
		pending[pg] = w[off+24 : off+24+ps]
		if commit := binary.BigEndian.Uint32(h[4:8]); commit != 0 {
			for k, v := range pending {
				db.wal[k] = v
			}
			pending = map[uint32][]byte{}
			db.nPages = commit
		}
		off += 24 + ps
	}
}

func (db *sqliteDB) page(n uint32) []byte {
	if p, ok := db.wal[n]; ok {
		return p
	}
	o := int(n-1) * db.pageSize
	if n == 0 || o+db.pageSize > len(db.main) {
		return nil
	}
	return db.main[o : o+db.pageSize]
}

func varint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return v<<8 | uint64(b[i]), 9
		}
		v = v<<7 | uint64(b[i]&0x7F)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return v, len(b)
}

type sqlRow struct {
	rowid int64
	vals  []interface{}
}

// scan recorre todas las filas de una tabla a partir de su página raíz.
func (db *sqliteDB) scan(root uint32, fn func(sqlRow)) {
	seen := map[uint32]bool{}
	var walk func(pg uint32)
	walk = func(pg uint32) {
		if seen[pg] || len(seen) > 200000 {
			return
		}
		seen[pg] = true
		p := db.page(pg)
		if p == nil {
			return
		}
		h := 0
		if pg == 1 {
			h = 100
		}
		typ := p[h]
		n := int(binary.BigEndian.Uint16(p[h+3 : h+5]))
		switch typ {
		case 0x05: // interior de tabla
			for i := 0; i < n; i++ {
				co := int(binary.BigEndian.Uint16(p[h+12+2*i:]))
				if co+4 > len(p) {
					continue
				}
				walk(binary.BigEndian.Uint32(p[co : co+4]))
			}
			walk(binary.BigEndian.Uint32(p[h+8 : h+12]))
		case 0x0D: // hoja de tabla
			for i := 0; i < n; i++ {
				co := int(binary.BigEndian.Uint16(p[h+8+2*i:]))
				if co >= len(p) {
					continue
				}
				if r, ok := db.leafCell(p, co); ok {
					fn(r)
				}
			}
		}
	}
	walk(root)
}

func (db *sqliteDB) leafCell(p []byte, co int) (sqlRow, bool) {
	size, a := varint(p[co:])
	rowid, b := varint(p[co+a:])
	start := co + a + b
	P := int(size)
	U := db.usable
	X := U - 35
	var payload []byte
	if P <= X {
		if start+P > len(p) {
			return sqlRow{}, false
		}
		payload = p[start : start+P]
	} else {
		M := ((U-12)*32/255 - 23)
		K := M + (P-M)%(U-4)
		local := M
		if K <= X {
			local = K
		}
		if start+local+4 > len(p) {
			return sqlRow{}, false
		}
		payload = append([]byte{}, p[start:start+local]...)
		next := binary.BigEndian.Uint32(p[start+local:])
		guard := 0
		for next != 0 && len(payload) < P && guard < 100000 {
			guard++
			op := db.page(next)
			if op == nil {
				break
			}
			take := U - 4
			if rem := P - len(payload); rem < take {
				take = rem
			}
			payload = append(payload, op[4:4+take]...)
			next = binary.BigEndian.Uint32(op[0:4])
		}
	}
	return sqlRow{int64(rowid), decodeRecord(payload)}, true
}

func decodeRecord(b []byte) []interface{} {
	hs, n := varint(b)
	if int(hs) > len(b) {
		return nil
	}
	var types []uint64
	for i := n; i < int(hs); {
		t, m := varint(b[i:])
		types = append(types, t)
		i += m
	}
	off := int(hs)
	out := make([]interface{}, 0, len(types))
	for _, t := range types {
		var sz int
		switch {
		case t == 0, t == 8, t == 9:
			sz = 0
		case t >= 1 && t <= 4:
			sz = int(t)
		case t == 5:
			sz = 6
		case t == 6, t == 7:
			sz = 8
		case t >= 12:
			sz = int(t-12) / 2
		}
		if off+sz > len(b) {
			out = append(out, nil)
			continue
		}
		d := b[off : off+sz]
		switch {
		case t == 0:
			out = append(out, nil)
		case t == 8:
			out = append(out, int64(0))
		case t == 9:
			out = append(out, int64(1))
		case t >= 1 && t <= 6:
			var v int64
			for _, c := range d {
				v = v<<8 | int64(c)
			}
			shift := uint(64 - 8*sz)
			v = v << shift >> shift // extensión de signo
			out = append(out, v)
		case t == 7:
			out = append(out, math.Float64frombits(binary.BigEndian.Uint64(d)))
		case t >= 12 && t%2 == 0:
			out = append(out, append([]byte{}, d...))
		default:
			out = append(out, string(d))
		}
		off += sz
	}
	return out
}

type sqlTable struct {
	root    uint32
	cols    []string
	rowidAt int // columna que es alias de rowid (-1 si ninguna)
}

func (db *sqliteDB) table(name string) (sqlTable, bool) {
	var res sqlTable
	found := false
	db.scan(1, func(r sqlRow) {
		if found || len(r.vals) < 5 {
			return
		}
		if t, _ := r.vals[0].(string); t != "table" {
			return
		}
		if n, _ := r.vals[1].(string); !strings.EqualFold(n, name) {
			return
		}
		root, _ := r.vals[3].(int64)
		sql, _ := r.vals[4].(string)
		res.root = uint32(root)
		res.cols, res.rowidAt = parseColumns(sql)
		found = true
	})
	return res, found
}

func parseColumns(sql string) ([]string, int) {
	i := strings.Index(sql, "(")
	j := strings.LastIndex(sql, ")")
	if i < 0 || j <= i {
		return nil, -1
	}
	body := sql[i+1 : j]
	var parts []string
	depth, last := 0, 0
	for k, ch := range body {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[last:k])
				last = k + 1
			}
		}
	}
	parts = append(parts, body[last:])
	var cols []string
	rowidAt := -1
	for _, p := range parts {
		p = strings.TrimSpace(p)
		up := strings.ToUpper(p)
		if strings.HasPrefix(up, "FOREIGN ") || strings.HasPrefix(up, "PRIMARY ") ||
			strings.HasPrefix(up, "UNIQUE") || strings.HasPrefix(up, "CHECK") || strings.HasPrefix(up, "CONSTRAINT ") {
			continue
		}
		f := strings.Fields(p)
		if len(f) == 0 {
			continue
		}
		name := strings.Trim(f[0], "\"`[]'")
		if len(f) >= 2 && strings.ToUpper(f[1]) == "INTEGER" && strings.Contains(up, "PRIMARY KEY") {
			rowidAt = len(cols)
		}
		cols = append(cols, name)
	}
	return cols, rowidAt
}

// rows devuelve las filas como mapas columna -> valor.
func (db *sqliteDB) rows(name string) []map[string]interface{} {
	t, ok := db.table(name)
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	db.scan(t.root, func(r sqlRow) {
		m := map[string]interface{}{}
		for i, c := range t.cols {
			if i == t.rowidAt {
				m[c] = r.rowid
			} else if i < len(r.vals) {
				m[c] = r.vals[i]
			}
		}
		out = append(out, m)
	})
	return out
}

func readShared(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, st.Size())
	n := 0
	for n < len(buf) {
		m, err := f.Read(buf[n:])
		n += m
		if err != nil || m == 0 {
			break
		}
	}
	return buf[:n], nil
}
