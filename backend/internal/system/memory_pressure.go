package system

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// MemoryPressure is the percentage of time stalled on memory over ten seconds.
// It is absent when the kernel does not supply a complete, valid PSI sample.
type MemoryPressure struct {
	SomeAvg10 float64 `json:"someAvg10"`
	FullAvg10 float64 `json:"fullAvg10"`
}

func parseMemoryPressure(data string) *MemoryPressure {
	values := make(map[string]float64)
	for line := range strings.SplitSeq(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || (fields[0] != "some" && fields[0] != "full") {
			continue
		}
		for _, field := range fields[1:] {
			if raw, ok := strings.CutPrefix(field, "avg10="); ok {
				value, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
					return nil
				}
				if _, duplicate := values[fields[0]]; duplicate {
					return nil
				}
				values[fields[0]] = value
			}
		}
	}
	some, someOK := values["some"]
	full, fullOK := values["full"]
	if !someOK || !fullOK || full > some {
		return nil
	}
	return &MemoryPressure{SomeAvg10: some, FullAvg10: full}
}

type memoryWarning struct {
	last, since time.Time
	low, active bool
}

func (w *memoryWarning) observe(mem Memory, now time.Time) bool {
	if mem.TotalBytes == 0 {
		*w = memoryWarning{}
		return false
	}
	low := mem.AvailableBytes <= memoryReserve(mem.TotalBytes)
	// An observation gap is not evidence of continuous shortage or recovery.
	if w.last.IsZero() || now.Before(w.last) || now.Sub(w.last) > 3*sampleInterval || low != w.low {
		w.since = now
	}
	w.last, w.low = now, low
	if low && now.Sub(w.since) >= 30*time.Second {
		w.active = true
	} else if !low && now.Sub(w.since) >= 60*time.Second {
		w.active = false
	}
	return w.active
}
