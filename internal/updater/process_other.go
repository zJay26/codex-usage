//go:build !windows && !linux && !darwin

package updater

func processAlive(pid int) bool { return pid > 0 }
