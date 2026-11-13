package server

import (
	"path/filepath"

	"dconnector/internal/config"
)

// Nombres del servicio de Windows.
const (
	ServiceName        = "dConnectorServer"
	ServiceDisplayName = "dConnector Servidor"
	ServiceDescription = "Servidor de dConnector: bandeja unificada de WhatsApp, Telegram, Messenger e Instagram con API HTTPS."
)

// ServiceInstallArgs devuelve los argumentos que quedan guardados en la línea de comandos del servicio.
// Si no se indicó --data, el servicio usa <programData>\dConnector (la cuenta del servicio no tiene un
// perfil de usuario con APPDATA propio).
func ServiceInstallArgs(cfg config.Server, programData string) []string {
	if cfg.Data == "" && programData != "" {
		cfg.Data = filepath.Join(programData, "dConnector")
	}
	return cfg.ServiceArgs()
}
