// Package buildinfo guarda datos fijados al compilar.
package buildinfo

// Version se fija con -ldflags "-X dconnector/internal/buildinfo.Version=0.1.0".
var Version = "dev"

// ProtocolVersion: versión del protocolo WebSocket (docs/PROTOCOLO-v1.md).
const ProtocolVersion = 1
