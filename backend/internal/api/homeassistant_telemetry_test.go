package api

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

func TestHAMetricUnknownAndMetadata(t *testing.T) {
	for _, value := range []any{nil, (*float64)(nil), math.NaN(), math.Inf(1), (*time.Time)(nil)} {
		if m := haMetric("load", "Load", value, "%", "", "measurement"); m.Value != nil {
			t.Fatalf("invalid measurement became known: %+v", m)
		}
	}
	zero := 0.0
	m := haMetric("load", "Load", &zero, "%", "", "measurement")
	if m.Value == nil || *m.Value != "0.00" {
		t.Fatal("measured zero lost")
	}
	for _, key := range []string{"on_battery", "shutdown"} {
		known := haMetric(key, key, true, "", "", "")
		unknown := haMetric(key, key, nil, "", "", "")
		if !known.Binary || !unknown.Binary {
			t.Fatal("binary discovery component changed when disconnected")
		}
	}
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.FixedZone("offset", 7200))
	stamp := haMetric("success", "Success", &at, "", "timestamp", "")
	if *stamp.Value != "2026-10-24T10:00:00Z" {
		t.Fatal("timestamp not UTC")
	}
	encoded, _ := json.Marshal(homeassistant.DeviceValues{Group: "gpu", ID: "pci", Name: "Card", Metrics: []homeassistant.MetricValue{{Name: "private measure"}}})
	if strings.Contains(string(encoded), "private measure") || strings.Contains(string(encoded), "Metrics") {
		t.Fatal("device inventory leaked measurement payload")
	}
}
