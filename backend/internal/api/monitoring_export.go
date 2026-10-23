package api

import (
	"encoding/csv"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func csvText(value string) string {
	if strings.HasPrefix(value, "=") || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "@") || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	return value
}
func (s *Server) handleMonitoringExport(w http.ResponseWriter, r *http.Request) {
	if s.opts.Monitoring == nil {
		featureUnavailable(w)
		return
	}
	hours, err := strconv.Atoi(r.URL.Query().Get("hours"))
	if err != nil {
		featureError(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "csv" && format != "json" {
		writeError(w, 400, "invalid_format", "choose csv or json")
		return
	}
	samples, err := s.opts.Monitoring.History(r.Context(), hours)
	if err != nil {
		featureError(w, err)
		return
	}
	rows := 0
	for _, sample := range samples {
		rows += 3 + len(sample.Disks)*3 + len(sample.Volumes)*2 + len(sample.Networks)*2
	}
	if rows > 250000 {
		writeError(w, 400, "export_limit", "choose a shorter time range")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="titanos-monitoring-%dh.%s"`, hours, format))
	w.Header().Set("Cache-Control", "no-store")
	if format == "json" {
		writeJSON(w, 200, samples)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	out := csv.NewWriter(w)
	if err := out.Write([]string{"at", "gap", "category", "device", "metric", "value", "unit"}); err != nil {
		return
	}
	for _, sample := range samples {
		if r.Context().Err() != nil {
			return
		}
		write := func(kind, device, metric, value, unit string) {
			_ = out.Write([]string{sample.At.UTC().Format(time.RFC3339), strconv.FormatBool(sample.Gap), kind, csvText(device), metric, value, unit})
		}
		number := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
		write("system", "", "cpu", number(sample.CPU), "percent")
		write("system", "", "ram_available", strconv.FormatUint(sample.RAMAvailable, 10), "bytes")
		write("system", "", "ram_total", strconv.FormatUint(sample.RAMTotal, 10), "bytes")
		for _, d := range sample.Disks {
			write("disk", d.Name, "read", number(d.ReadBPS), "bytes/s")
			write("disk", d.Name, "write", number(d.WriteBPS), "bytes/s")
			value := ""
			if d.LatencyMS != nil {
				value = number(*d.LatencyMS)
			}
			write("disk", d.Name, "latency", value, "ms")
		}
		for _, v := range sample.Volumes {
			write("volume", v.Name, "used", strconv.FormatUint(v.Used, 10), "bytes")
			write("volume", v.Name, "total", strconv.FormatUint(v.Total, 10), "bytes")
		}
		for _, n := range sample.Networks {
			write("network", n.Name, "receive", number(n.Receive), "bytes/s")
			write("network", n.Name, "send", number(n.Send), "bytes/s")
		}
		out.Flush()
		if out.Error() != nil {
			return
		}
	}
	out.Flush()
}

func (s *Server) handleCapacityTrends(w http.ResponseWriter, r *http.Request) {
	if s.opts.Monitoring == nil {
		featureUnavailable(w)
		return
	}
	samples, err := s.opts.Monitoring.History(r.Context(), 720)
	if err != nil {
		featureError(w, err)
		return
	}
	writeJSON(w, 200, monitoring.CapacityTrends(samples, time.Now()))
}
