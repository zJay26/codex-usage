package server

import "time"

type hourWindow struct {
	Date  string    `json:"date"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Hours int       `json:"complete_hours"`
}

func makeHourWindow(date string, loc *time.Location, now time.Time, complete bool) hourWindow {
	start, _ := time.ParseInLocation("2006-01-02", date, loc)
	end := start.AddDate(0, 0, 1)
	if complete {
		now = now.In(loc)
		current := now.Add(-time.Duration(now.Minute())*time.Minute - time.Duration(now.Second())*time.Second - time.Duration(now.Nanosecond()))
		if current.Before(end) {
			end = current
		}
		if end.Before(start) {
			end = start
		}
	}
	return hourWindow{Date: date, Start: start.UTC(), End: end.UTC(), Hours: int(end.Sub(start) / time.Hour)}
}
