package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The power the CPU packages draw, from the energy counters of the kernel
// (powercap, RAPL: Intel, and AMD since Zen). It is the CPU only, not the
// whole NAS; disks, fans and the power supply are not counted. Unknown
// (nil) on machines without the counters and in virtual machines.

type raplZone struct {
	energy, max string
}

// raplZones finds the top-level package zones (intel-rapl:N, not the
// sub-zones intel-rapl:N:M), the same for AMD.
func raplZones(sys string) []raplZone {
	paths, _ := filepath.Glob(filepath.Join(sys, "class/powercap/*rapl:[0-9]*"))
	var zones []raplZone
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.Count(name, ":") != 1 {
			continue
		}
		if !strings.HasPrefix(readTrimmed(filepath.Join(path, "name")), "package") {
			continue
		}
		zones = append(zones, raplZone{energy: filepath.Join(path, "energy_uj"), max: filepath.Join(path, "max_energy_range_uj")})
	}
	return zones
}

type raplSample struct {
	at     time.Time
	energy []uint64
}

func readRapl(zones []raplZone, now time.Time) (raplSample, bool) {
	s := raplSample{at: now}
	for _, z := range zones {
		data, err := os.ReadFile(z.energy)
		if err != nil {
			return s, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return s, false
		}
		s.energy = append(s.energy, v)
	}
	return s, len(zones) > 0
}

// raplWatts is the average power between two samples; the counters wrap
// at max_energy_range_uj.
func raplWatts(zones []raplZone, before, after raplSample) *float64 {
	seconds := after.at.Sub(before.at).Seconds()
	if seconds <= 0 || len(before.energy) != len(after.energy) || len(after.energy) != len(zones) {
		return nil
	}
	var total float64
	for i := range zones {
		delta := after.energy[i] - before.energy[i]
		if after.energy[i] < before.energy[i] {
			limit, err := strconv.ParseUint(readTrimmed(zones[i].max), 10, 64)
			if err != nil || limit == 0 {
				return nil
			}
			delta = limit - before.energy[i] + after.energy[i]
		}
		total += float64(delta) / 1e6
	}
	watts := total / seconds
	if watts < 0 || watts > 2000 {
		return nil
	}
	return &watts
}
