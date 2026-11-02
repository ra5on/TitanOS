package nvr

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Arming decides when movement is reported (Settings › Notifications,
// section "Cameras"). The NVR page only shows the state with a switch.
type Arming struct {
	// Mode is "off", "on" or "schedule".
	Mode string `json:"mode"`
	// Schedule lists the times armed in mode "schedule".
	Schedule []Window `json:"schedule,omitempty"`
	// Cameras limits reporting to these cameras; empty means all cameras
	// with motion detection.
	Cameras []string `json:"cameras,omitempty"`
	// QuietMinutes is the least time between two notices of one camera;
	// 0 means 5.
	QuietMinutes int `json:"quietMinutes,omitempty"`
}

// Window is a time span on some weekdays (0 = Sunday). A window whose end
// is before its start runs over midnight.
type Window struct {
	Days []int  `json:"days"`
	From string `json:"from"` // "22:00"
	To   string `json:"to"`   // "06:00"
}

const maxWindows = 14

func clock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 || len(m) != 2 {
		return 0, false
	}
	return hh*60 + mm, true
}

// Validate checks the setting.
func (a Arming) Validate() error {
	switch a.Mode {
	case "", "off", "on", "schedule":
	default:
		return fmt.Errorf("%w: arming mode", ErrInvalid)
	}
	if len(a.Schedule) > maxWindows || len(a.Cameras) > maxCameras || a.QuietMinutes < 0 || a.QuietMinutes > 24*60 {
		return fmt.Errorf("%w: arming", ErrInvalid)
	}
	for _, w := range a.Schedule {
		_, ok1 := clock(w.From)
		_, ok2 := clock(w.To)
		if !ok1 || !ok2 || len(w.Days) == 0 || len(w.Days) > 7 {
			return fmt.Errorf("%w: arming window", ErrInvalid)
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("%w: arming weekday", ErrInvalid)
			}
		}
	}
	return nil
}

// Active reports whether the NVR is armed at that moment.
func (a Arming) Active(now time.Time) bool {
	switch a.Mode {
	case "on":
		return true
	case "schedule":
		minute := now.Hour()*60 + now.Minute()
		day := int(now.Weekday())
		yesterday := (day + 6) % 7
		for _, w := range a.Schedule {
			from, _ := clock(w.From)
			to, _ := clock(w.To)
			switch {
			case from < to:
				if slices.Contains(w.Days, day) && minute >= from && minute < to {
					return true
				}
			case from > to: // over midnight: the evening part on the day, the morning part the day after
				if slices.Contains(w.Days, day) && minute >= from || slices.Contains(w.Days, yesterday) && minute < to {
					return true
				}
			}
		}
	}
	return false
}

// Watches reports whether notices of this camera are wanted.
func (a Arming) Watches(camera string) bool {
	return len(a.Cameras) == 0 || slices.Contains(a.Cameras, camera)
}

func (a Arming) quiet() time.Duration {
	if a.QuietMinutes <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(a.QuietMinutes) * time.Minute
}
