package ws

import _ "embed"

// protocolDoc: copia embebida de docs/PROTOCOLO-v1.md (el documento canónico). La interfaz lo
// muestra completo para que quien lo lea no necesite el archivo; doc_test.go falla si las dos
// copias difieren: al editar el documento, hay que copiarlo también aquí.
//
//go:embed PROTOCOLO-v1.md
var protocolDoc string

// ProtocolDoc devuelve el documento del protocolo v1 (Markdown).
func ProtocolDoc() string { return protocolDoc }
