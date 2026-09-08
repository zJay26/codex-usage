package timezone

import (
	"os"
	"strings"
	"time"
	_ "time/tzdata"
)

func Detect() string {
	if name := strings.TrimPrefix(os.Getenv("TZ"), ":"); name != "" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	if name := time.Local.String(); name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return systemZone()
}
