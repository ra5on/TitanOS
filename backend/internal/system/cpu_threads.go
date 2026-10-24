package system

import (
	"sort"
	"strconv"
	"strings"
)

func parseCPUThreads(data string) map[string]cpuTimes {
	result := map[string]cpuTimes{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") || fields[0] == "cpu" {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu")); err != nil {
			continue
		}
		result[fields[0]] = parseCPULine(line)
	}
	return result
}
func sampleCPUThreads(before, after map[string]cpuTimes) []ThreadUsage {
	ids := make([]string, 0, len(after))
	for id := range after {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { a, _ := strconv.Atoi(ids[i][3:]); b, _ := strconv.Atoi(ids[j][3:]); return a < b })
	out := make([]ThreadUsage, 0, len(ids))
	for _, id := range ids {
		var value *float64
		if prev, ok := before[id]; ok && after[id].total > prev.total {
			n := cpuUsage(prev, after[id])
			value = &n
		}
		out = append(out, ThreadUsage{ID: id, UsagePercent: value})
	}
	return out
}
