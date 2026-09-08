package timezone

import "golang.org/x/sys/windows/registry"

func systemZone() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, registry.QUERY_VALUE)
	if err != nil {
		return "UTC"
	}
	defer key.Close()
	name, _, err := key.GetStringValue("TimeZoneKeyName")
	if err == nil {
		if zone := windowsZones[name]; zone != "" {
			return zone
		}
	}
	return "UTC"
}
