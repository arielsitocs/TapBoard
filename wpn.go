package main

// Lectura de notificaciones de WhatsApp desde la base de datos de
// notificaciones de Windows (wpndatabase.db).

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf16"
)

type Notif struct {
	Title   string
	Body    string
	Arrival int64
	Tag     string
	Group   string
}

var reGeneric = regexp.MustCompile(`(?i)^(\d+\s+)?(nuevos?\s+)?(mensajes?|messages?)(\s+nuevos?|\s+new)?(\s+de\s+\d+\s+chats?|\s+from\s+\d+\s+chats?)?\.?$|^(new|nuevo)\s+(message|mensaje)s?\.?$|^tienes\s+.*mensajes?`)

// isGenericNotif: avisos tipo "Nuevo mensaje" / "3 mensajes nuevos" sin el texto real.
func isGenericNotif(n Notif) bool {
	b := strings.TrimSpace(n.Body)
	if b == "" {
		return true
	}
	return reGeneric.MatchString(b)
}

func sameSender(a, b Notif) bool {
	return strings.EqualFold(strings.TrimSpace(a.Title), strings.TrimSpace(b.Title)) || strings.EqualFold(a.Title, "WhatsApp") || strings.EqualFold(b.Title, "WhatsApp")
}

const dupWindow = 30 * 10_000_000 // 30 s en unidades de 100 ns

// dedupeNotifs quita avisos genéricos cuando existe el mensaje real cerca en el
// tiempo, y deja solo la última versión de una notificación actualizada (mismo tag).
func dedupeNotifs(list []Notif) []Notif {
	// misma etiqueta/grupo = la misma notificación actualizada
	lastByTag := map[string]int{}
	for i, n := range list {
		if n.Tag != "" {
			lastByTag[n.Tag+"|"+n.Group] = i
		}
	}
	var out []Notif
	for i, n := range list {
		if n.Tag != "" && lastByTag[n.Tag+"|"+n.Group] != i {
			continue
		}
		if isGenericNotif(n) {
			dup := false
			for _, o := range list {
				if !isGenericNotif(o) && sameSender(n, o) && absI64(o.Arrival-n.Arrival) <= dupWindow {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func absI64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

var reToastText = regexp.MustCompile(`(?s)<text[^>]*>(.*?)</text>`)

func toInt(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

func toStr(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		// UTF-16LE (con muchos ceros) o UTF-8
		if len(x) >= 4 && x[1] == 0 && x[3] == 0 {
			u := make([]uint16, len(x)/2)
			for i := range u {
				u[i] = uint16(x[2*i]) | uint16(x[2*i+1])<<8
			}
			return string(utf16.Decode(u))
		}
		return string(x)
	}
	return ""
}

// sinEmojis quita emojis y símbolos que GDI+ no sabe dibujar (salen como cuadros).
func sinEmojis(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0x200D || (r >= 0xFE00 && r <= 0xFE0F) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isWhatsApp(s string) bool {
	return strings.Contains(strings.ToLower(s), "whatsapp")
}

// readWhatsApp devuelve las notificaciones de WhatsApp con ArrivalTime > since
// (ordenadas de más antigua a más nueva) y el ArrivalTime máximo encontrado.
func readWhatsApp(path string, since int64) ([]Notif, int64, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, since, err
	}
	handlers := map[int64]bool{}
	for _, h := range db.rows("NotificationHandler") {
		if isWhatsApp(toStr(h["PrimaryId"])) {
			handlers[toInt(h["RecordId"])] = true
		}
	}
	maxA := since
	var out []Notif
	for _, n := range db.rows("Notification") {
		if t := strings.ToLower(toStr(n["Type"])); t != "" && t != "toast" {
			continue
		}
		payload := toStr(n["Payload"])
		if !handlers[toInt(n["HandlerId"])] && !isWhatsApp(payload) {
			continue
		}
		a := toInt(n["ArrivalTime"])
		if a > maxA {
			maxA = a
		}
		if a <= since {
			continue
		}
		var texts []string
		for _, m := range reToastText.FindAllStringSubmatch(payload, -1) {
			t := sinEmojis(html.UnescapeString(m[1]))
			if t != "" {
				texts = append(texts, t)
			}
		}
		if len(texts) == 0 {
			continue
		}
		nt := Notif{Title: texts[0], Arrival: a, Tag: toStr(n["Tag"]), Group: toStr(n["Group"])}
		if len(texts) > 1 {
			nt.Body = strings.Join(texts[1:], " ")
		}
		out = append(out, nt)
	}
	// ordenar por llegada (inserción simple, son pocas)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Arrival < out[j-1].Arrival; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, maxA, nil
}
