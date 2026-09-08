package updater

import "golang.org/x/sys/windows"

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	result, err := windows.WaitForSingleObject(h, 0)
	return err != nil || result == uint32(windows.WAIT_TIMEOUT)
}
