package homeassistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestHistoryFiltersAndRange(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("filter_entity_id") != "sensor.room" {
			t.Error("unfiltered request")
		}
		w.Write([]byte(`[[{"entity_id":"sensor.room","state":"20"},{"state":"21"}],[{"entity_id":"sensor.private","state":"secret"}]]`))
	}))
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "test", Entities: []string{"sensor.room"}}); err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	r, err := m.History(context.Background(), "sensor.room", end.Add(-time.Hour), end, 0)
	if err != nil || len(r.States) != 2 || r.States[1].EntityID != "sensor.room" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = m.History(context.Background(), "sensor.private", end.Add(-time.Hour), end, 0); err == nil {
		t.Fatal("private history")
	}
	if _, err = m.History(context.Background(), "sensor.room", end.Add(-25*time.Hour), end, 0); err == nil {
		t.Fatal("unbounded history")
	}
	if calls != 1 {
		t.Fatal("invalid requests reached HA")
	}
}
