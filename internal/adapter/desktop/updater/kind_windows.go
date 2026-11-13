//go:build windows

package updater

import "golang.org/x/sys/windows/registry"

func registryInstalled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Darkmelon\dConnector`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("installed")
	return err == nil && v == 1
}
