//go:build windows

package main

import (
	"os"
	"testing"
	"time"
)

func timeNowPlusHour() time.Time { return time.Now().Add(time.Hour) }

func TestDPAPIRoundTrip(t *testing.T) {
	secret := []byte("token-de-prueba-123")
	enc, err := dpapi(pCryptProtectData, secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) == string(secret) {
		t.Fatal("no se cifró")
	}
	dec, err := dpapi(pCryptUnprotectData, enc)
	if err != nil || string(dec) != string(secret) {
		t.Fatalf("descifrado: %q %v", dec, err)
	}
}

func TestRefreshTokenFile(t *testing.T) {
	dir := t.TempDir()
	old := os.Getenv("APPDATA")
	os.Setenv("APPDATA", dir)
	defer os.Setenv("APPDATA", old)
	if spotifyConnected() {
		t.Fatal("conectado sin token")
	}
	saveRefreshToken("abc")
	if !spotifyConnected() || loadRefreshToken() != "abc" {
		t.Fatal("no guardó/leyó el token")
	}
	b, _ := os.ReadFile(spTokenPath())
	if string(b) == "abc" {
		t.Fatal("el token quedó en texto plano")
	}
	disconnectSpotify()
	if spotifyConnected() {
		t.Fatal("sigue conectado después de desconectar")
	}
}

func TestSpAPIOnlySpotify(t *testing.T) {
	spMu.Lock()
	spAccess = "x"
	spAccessExp = timeNowPlusHour()
	spMu.Unlock()
	if _, err := spAPI("GET", "https://evil.example.com/v1/me", nil, nil); err == nil {
		t.Fatal("aceptó otro servidor")
	}
}
