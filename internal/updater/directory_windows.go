package updater

import "golang.org/x/sys/windows"

func userDownloadsDirectory() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
}
