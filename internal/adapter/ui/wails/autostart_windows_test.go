//go:build windows

package wails

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutostartCommand(t *testing.T) {
	require.Equal(t, `"C:\Program Files\dConnector\dConnector.exe"`, autostartCommand(`C:\Program Files\dConnector\dConnector.exe`, false))
	require.Equal(t, `"C:\x\dConnector.exe" --minimized`, autostartCommand(`C:\x\dConnector.exe`, true))
}
