package platform

func ManagedUpdateService() bool { return false }
func StartUpdateHelper(executable, job, stateDir string, managed bool) error {
	return StartDetached(executable, "_apply-update", job)
}
func StopForUpdate(executable, stateDir string, managed bool) error {
	return stopWindowsExecutable(executable)
}
func StartAfterUpdate(executable string, managed bool) error {
	return StartDetached(executable, "daemon")
}
