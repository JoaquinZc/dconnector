package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pruneTmp solo borra lo viejo: los temporales en curso de un servidor vivo no se tocan.
func TestPruneTmpKeepsRecentFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "viejo")
	recent := filepath.Join(dir, "up-en-curso")
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	pruneTmp(dir, 24*time.Hour)
	if _, err := os.Stat(old); err == nil {
		t.Fatal("el archivo viejo debía borrarse")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("el archivo reciente no debía borrarse")
	}
}
