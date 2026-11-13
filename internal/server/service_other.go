//go:build !windows

package server

import (
	"errors"
	"io"

	"dconnector/internal/config"
)

var errNoWindows = errors.New("el servicio de Windows solo existe en Windows; en Linux use la unidad de systemd (deploy/systemd/dconnector.service) o Docker")

// IsWindowsService siempre es falso fuera de Windows.
func IsWindowsService() bool { return false }

// RunAsService no está disponible fuera de Windows.
func RunAsService(config.Server) error { return errNoWindows }

// ControlService no está disponible fuera de Windows.
func ControlService(string, config.Server, io.Writer) error { return errNoWindows }
