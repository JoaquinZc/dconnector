//go:build !windows

package updater

import (
	"errors"
	"time"
)

var errUnsupported = errors.New("la actualización automática solo existe en Windows")

func startDetached(string, []string) error    { return errUnsupported }
func waitParent(int, time.Duration) error     { return errUnsupported }
func waitServers(string, time.Duration) error { return errUnsupported }
func runMSI(string, string) (int, error)      { return -1, errUnsupported }
func relaunch(string, []string) error         { return errUnsupported }
func showMessage(string, string)              {}
func openFolder(string)                       {}
