//go:build !windows && !linux && !darwin

package platform

import "fmt"

func ManagedUpdateService() bool { return false }
func StartUpdateHelper(executable, job, stateDir string, managed bool) error {
	return fmt.Errorf("updates unsupported on this platform")
}
func StopForUpdate(executable, stateDir string, managed bool) error {
	return fmt.Errorf("updates unsupported on this platform")
}
func StartAfterUpdate(executable string, managed bool) error {
	return fmt.Errorf("updates unsupported on this platform")
}
