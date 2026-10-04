package config

import (
	"io"
	"strings"
	"testing"
)

func TestSaveTokenReplacement(t *testing.T) {
	for _, name := range []string{"HOME", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	t.Setenv("OKUPTIME_TOKEN", "")
	for _, token := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		if err := SaveToken(strings.NewReader(token+"\n"), io.Discard); err != nil {
			t.Fatal(err)
		}
		got, err := Token()
		if err != nil || got != token {
			t.Fatalf("token replacement failed: %v", err)
		}
	}
	if err := SaveToken(strings.NewReader("invalid"), io.Discard); err != ErrInvalidToken {
		t.Fatal("invalid token accepted")
	}
	got, err := Token()
	if err != nil || got != strings.Repeat("b", 64) {
		t.Fatal("invalid token overwrote saved token")
	}
}
