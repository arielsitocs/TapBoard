//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpectedSum(t *testing.T) {
	data := []byte("contenido de prueba")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(good + "  TapBoard.exe\n"))
		case "/bad":
			w.Write([]byte("no es una huella"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	got, err := expectedSum(srv.URL + "/ok")
	if err != nil || got != good {
		t.Fatalf("huella válida: got %q err %v", got, err)
	}
	if _, err := expectedSum(srv.URL + "/bad"); err == nil {
		t.Fatal("aceptó una huella mal formada")
	}
	if _, err := expectedSum(srv.URL + "/missing"); err == nil {
		t.Fatal("aceptó una huella que no existe")
	}
}

func TestDownloadPrefix(t *testing.T) {
	ok := "https://github.com/arielsitocs/TapBoard/releases/download/v3.0.0/TapBoard.exe"
	if len(ok) < len(updDownloadPrefix) || ok[:len(updDownloadPrefix)] != updDownloadPrefix {
		t.Fatal("rechaza la URL real de la release")
	}
	bad := "https://github.com/otro/TapBoard/releases/download/v3.0.0/TapBoard.exe"
	if bad[:len(updDownloadPrefix)] == updDownloadPrefix {
		t.Fatal("acepta otro repositorio")
	}
}
