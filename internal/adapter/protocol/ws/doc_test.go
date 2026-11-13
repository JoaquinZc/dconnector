package ws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// La copia embebida debe ser idéntica al documento canónico de docs/.
func TestEmbeddedProtocolDocMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "PROTOCOLO-v1.md"))
	if err != nil {
		t.Fatalf("no se pudo leer docs/PROTOCOLO-v1.md: %v", err)
	}
	// Git en Windows puede convertir los saltos de línea; lo que importa es el texto.
	norm := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
	if norm(string(canonical)) != norm(ProtocolDoc()) {
		t.Fatal("internal/adapter/protocol/ws/PROTOCOLO-v1.md difiere de docs/PROTOCOLO-v1.md: " +
			"copia el documento canónico (cp docs/PROTOCOLO-v1.md internal/adapter/protocol/ws/PROTOCOLO-v1.md)")
	}
}

func TestProtocolDocIsComplete(t *testing.T) {
	doc := ProtocolDoc()
	for _, want := range []string{"# Protocolo dConnector v1", "## 1. Conexión", "## 10. Servidor de ejemplo", "contactScope"} {
		if !strings.Contains(doc, want) {
			t.Errorf("el documento embebido no contiene %q", want)
		}
	}
}
