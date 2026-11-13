//go:build !windows

package updater

func registryInstalled() bool { return false }
