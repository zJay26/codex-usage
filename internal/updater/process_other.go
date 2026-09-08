//go:build !windows && !linux

package updater

func processAlive(pid int) bool { return pid > 0 }
