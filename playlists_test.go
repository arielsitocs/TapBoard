//go:build windows

package main

import (
	"os"
	"testing"
)

func TestParseSpotifyLink(t *testing.T) {
	cases := []struct {
		in       string
		kind, id string
		ok       bool
	}{
		{"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc123", "playlist", "37i9dQZF1DXcBWIGoYBM5M", true},
		{"https://open.spotify.com/intl-es/album/0ETFjACtuP2ADo6LFhL6HN", "album", "0ETFjACtuP2ADo6LFhL6HN", true},
		{"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M", "playlist", "37i9dQZF1DXcBWIGoYBM5M", true},
		{"  https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M  ", "playlist", "37i9dQZF1DXcBWIGoYBM5M", true},
		{"https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC", "", "", false}, // una canción, no playlist
		{"https://evil.com/playlist/37i9dQZF1DXcBWIGoYBM5M", "", "", false},
		{"https://open.spotify.com/playlist/../../x", "", "", false},
		{"hola", "", "", false},
	}
	for _, c := range cases {
		p, ok := parseSpotifyLink(c.in)
		if ok != c.ok || (ok && (p.Kind != c.kind || p.ID != c.id)) {
			t.Errorf("%q → %+v %v", c.in, p, ok)
		}
	}
}

// TestSeedPlaylists baja nombre y portada de playlists públicas a la carpeta
// APPDATA actual (para capturas del modo demo). Solo con PL_SEED=1.
func TestSeedPlaylists(t *testing.T) {
	if os.Getenv("PL_SEED") == "" {
		t.Skip("PL_SEED no definido")
	}
	cfg = defaultConfig()
	for _, link := range []string{
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/playlist/37i9dQZF1DX10zKzsJ2jva",
		"https://open.spotify.com/album/0ETFjACtuP2ADo6LFhL6HN",
	} {
		p, _ := parseSpotifyLink(link)
		fetchPlaylistMeta(p)
		m := <-plCh
		if !m.ok {
			t.Errorf("no se pudo leer %s", link)
			continue
		}
		p.Name = m.name
		cfg.Playlists = append(cfg.Playlists, p)
		if _, err := os.Stat(p.coverPath()); err != nil {
			t.Errorf("sin portada: %s", m.name)
		}
		t.Logf("%s → %q", link, m.name)
	}
	saveConfig(cfg)
}
