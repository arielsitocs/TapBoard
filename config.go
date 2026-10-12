package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Monitor           int         `json:"monitor"`
	SiempreEncima     bool        `json:"alwaysOnTop"`
	Carpeta           string      `json:"folder"`
	DispositivosAudio []string    `json:"audioDevices"`
	AccionLuna        string      `json:"moonAction"`
	ConfirmarApagar   bool        `json:"confirmShutdown"`
	ConfirmarLuna     bool        `json:"confirmMoon"`
	MostrarSpotify    bool        `json:"showNowPlaying"`
	WhatsApp          bool        `json:"whatsAppNotifications"`
	WhatsAppTexto     bool        `json:"whatsAppShowMessage"`
	Botones           []BotonP2   `json:"buttons"`
	Sonidos           bool        `json:"tapSound"`
	PaginasExtra      [][]BotonP2 `json:"extraPages"`
	MainButtons       []BotonP2   `json:"mainButtons"`
	Theme             string      `json:"theme"`
	IdleMinutes       int         `json:"idleMinutes"`
	CheckUpdates      bool        `json:"checkUpdates"`
	AppPages          []AppPage   `json:"appPages"`
	SeenVersion       string      `json:"seenVersion"`
}

func defaultConfig() Config {
	return Config{
		Monitor:           0,
		SiempreEncima:     true,
		Carpeta:           "",
		DispositivosAudio: []string{},
		AccionLuna:        "sleep",
		ConfirmarApagar:   true,
		ConfirmarLuna:     true,
		MostrarSpotify:    true,
		WhatsApp:          true,
		WhatsAppTexto:     true,
		Botones:           defaultBotones(),
		Sonidos:           true,
		MainButtons:       defaultMainButtons(),
		Theme:             "dark",
		IdleMinutes:       3,
		CheckUpdates:      true,
	}
}

func configDir() string {
	d := os.Getenv("APPDATA")
	if d == "" {
		d, _ = os.UserConfigDir()
	}
	return filepath.Join(d, "TapBoard")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

func loadConfig() Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err != nil {
		// migrar desde el nombre antiguo (Panel Táctil)
		old := filepath.Join(filepath.Dir(configDir()), "PanelTactil", "config.json")
		if ob, oerr := os.ReadFile(old); oerr == nil {
			b, err = ob, nil
		}
	}
	if err != nil {
		saveConfig(cfg)
		return cfg
	}
	cfg.Botones = nil // evitar que se mezclen con los valores por defecto
	cfg.MainButtons = nil
	bad := json.Unmarshal(migrateConfig(b), &cfg) != nil
	for i := range cfg.Botones {
		if cfg.Botones[i].Tipo == "app" {
			cfg.Botones[i].Accion = ""
		}
	}
	if cfg.DispositivosAudio == nil {
		cfg.DispositivosAudio = []string{}
	}
	for len(cfg.MainButtons) < 5 {
		cfg.MainButtons = append(cfg.MainButtons, defaultMainButtons()[len(cfg.MainButtons)])
	}
	for len(cfg.Botones) < 8 {
		cfg.Botones = append(cfg.Botones, defaultBotones()[len(cfg.Botones)])
	}
	if !bad {
		saveConfig(cfg) // agrega opciones nuevas y quita las antiguas
	}
	return cfg
}

func saveConfig(cfg Config) {
	_ = os.MkdirAll(configDir(), 0o755)
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(configPath(), b, 0o644)
}

// migrateConfig traduce un config.json antiguo (claves en español) al formato
// actual en inglés. Si ya está en inglés, lo deja igual.
func migrateConfig(b []byte) []byte {
	var raw map[string]interface{}
	if json.Unmarshal(b, &raw) != nil {
		return b
	}
	keys := map[string]string{
		"siempreEncima": "alwaysOnTop", "carpeta": "folder", "dispositivosAudio": "audioDevices",
		"accionLuna": "moonAction", "confirmarApagar": "confirmShutdown", "confirmarLuna": "confirmMoon",
		"mostrarSpotify": "showNowPlaying", "notificacionesWhatsApp": "whatsAppNotifications",
		"whatsAppMostrarMensaje": "whatsAppShowMessage", "botonesPagina2": "buttons",
		"sonidoAlTocar": "tapSound", "paginasExtra": "extraPages",
	}
	changed := false
	for old, nw := range keys {
		if v, ok := raw[old]; ok {
			if _, exists := raw[nw]; !exists {
				raw[nw] = v
			}
			delete(raw, old)
			changed = true
		}
	}
	for _, k := range []string{"ciudad", "latitud", "longitud"} {
		if _, ok := raw[k]; ok {
			delete(raw, k)
			changed = true
		}
	}
	switch raw["moonAction"] {
	case "suspender":
		raw["moonAction"], changed = "sleep", true
	case "apagarPantalla":
		raw["moonAction"], changed = "screenOff", true
	}
	actions := map[string]string{"tareas": "taskManager", "calculadora": "calculator", "explorador": "explorer",
		"configuracion": "settings", "escritorio": "desktop", "portapapeles": "clipboard", "emojis": "emoji",
		"bloquear": "lock", "vacio": "empty"}
	fixButton := func(v interface{}) {
		m, ok := v.(map[string]interface{})
		if !ok {
			return
		}
		for old, nw := range map[string]string{"tipo": "type", "accion": "action", "nombre": "name", "ruta": "path"} {
			if x, ok := m[old]; ok {
				m[nw] = x
				delete(m, old)
				changed = true
			}
		}
		if m["type"] == "accion" {
			m["type"] = "action"
		}
		if a, ok := m["action"].(string); ok {
			if n, ok := actions[a]; ok {
				m["action"] = n
			}
		}
		if m["type"] == "app" {
			delete(m, "action")
		}
	}
	if arr, ok := raw["buttons"].([]interface{}); ok {
		for _, v := range arr {
			fixButton(v)
		}
	}
	if pages, ok := raw["extraPages"].([]interface{}); ok {
		for _, p := range pages {
			if arr, ok := p.([]interface{}); ok {
				for _, v := range arr {
					fixButton(v)
				}
			}
		}
	}
	if !changed {
		return b
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return b
	}
	return out
}
