package nvr

// The rest of DevFrigate (dev.go): events, recordings, playback, exports,
// PTZ, faces, plates and the WebSocket, shaped like Frigate 0.18. Used by
// development mode and tests only.

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// devmedia holds a four-second test picture as MP4 and as HLS (fMP4):
// what development mode plays for every clip, export and recording.
//
//go:embed devmedia
var devmedia embed.FS

func devFile(name string) []byte {
	data, err := devmedia.ReadFile("devmedia/" + name)
	if err != nil {
		panic(err) // embedded at build time
	}
	return data
}

// devEvent is an event of the stand-in.
type devEvent struct {
	ID, Camera, Label, SubLabel, Plate string
	Start                              float64
	End                                *float64
	Score                              float64
	Kept, Reviewed, NoSnapshot         bool
}

func (e devEvent) json() map[string]any {
	out := map[string]any{
		"id": e.ID, "camera": e.Camera, "label": e.Label, "sub_label": nil, "zones": []string{},
		"start_time": e.Start, "end_time": e.End, "has_clip": true, "has_snapshot": !e.NoSnapshot,
		"retain_indefinitely": e.Kept, "top_score": nil, "false_positive": nil, "plus_id": nil,
		"data": map[string]any{"score": e.Score, "top_score": e.Score, "type": "object"},
	}
	if e.SubLabel != "" {
		out["sub_label"] = e.SubLabel
	}
	if e.Plate != "" {
		out["data"].(map[string]any)["recognized_license_plate"] = e.Plate
	}
	return out
}

// devHour is one hour of recordings of a camera.
type devHour struct {
	Start   float64 // Unix seconds, a full hour
	Seconds float64 // recorded seconds in it, from its start
	MB      float64
}

// devExport is an export of the stand-in.
type devExport struct {
	ID, Camera, Name string
	Date             float64
	// State is "done", "running" or "failed".
	State string
	Size  int
}

// DevCommand is a command the stand-in got over its WebSocket.
type DevCommand struct{ Topic, Payload string }

// devData is the state of the stand-in beyond dev.go (guarded by
// DevFrigate.mu).
type devData struct {
	seeded   bool
	events   []devEvent
	hours    map[string][]devHour
	exports  []devExport
	faces    map[string][]string
	plates   []string
	commands []DevCommand
	sockets  map[*websocket.Conn]struct{}
	nextID   int
	// deleted are the spans DELETE /api/recordings got.
	deleted [][2]float64
}

// Seed fills the stand-in with example data around now for the given
// cameras: events of two days (one going on, one kept, a known face, a
// plate), recordings of three days with gaps, three exports (done,
// running, failed), two persons and two unassigned faces.
func (f *DevFrigate) Seed(cameras []Camera, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &f.data
	d.seeded = true
	d.events, d.hours, d.exports = nil, map[string][]devHour{}, nil
	d.faces = map[string][]string{
		"Anna":  {"Anna-1731000000.1.webp", "Anna-1731000100.2.webp"},
		"Ben":   {"Ben-1731000200.3.webp"},
		"train": {},
	}
	d.plates = []string{"M-AB 1234", "B-XY 987"}
	hour := float64(now.Truncate(time.Hour).Unix())
	t := float64(now.Unix())
	n := 0
	add := func(camera, label string, ago, length float64, edit func(*devEvent)) {
		n++
		start := t - ago
		end := start + length
		e := devEvent{ID: fmt.Sprintf("%.6f-dev%03d", start, n), Camera: camera, Label: label, Start: start, End: &end, Score: 0.6 + float64(n%4)/10}
		if edit != nil {
			edit(&e)
		}
		d.events = append(d.events, e)
	}
	for i, c := range cameras {
		if !c.Enabled {
			continue
		}
		// Recordings: three days back; a gap of five hours yesterday and
		// one missing quarter of an hour in between.
		for h := 72; h >= 0; h-- {
			if h >= 20+i && h < 25+i {
				continue
			}
			entry := devHour{Start: hour - float64(h)*3600, Seconds: 3600, MB: 450}
			if h == 0 {
				entry.Seconds = t - hour
				entry.MB = 450 * entry.Seconds / 3600
			}
			if h == 40 {
				entry.Seconds, entry.MB = 2700, 340
			}
			if entry.Seconds < 1 {
				continue
			}
			d.hours[c.ID] = append(d.hours[c.ID], entry)
		}
		labels := c.Detect.Objects
		if len(labels) == 0 {
			continue
		}
		for k := range 6 {
			add(c.ID, labels[k%len(labels)], float64(900+k*7200+i*600), float64(12+k*5), func(e *devEvent) { e.Reviewed = k >= 3 })
		}
		if c.Detect.Faces {
			add(c.ID, "person", 3000, 20, func(e *devEvent) { e.SubLabel = "Anna" })
			at := fmt.Sprintf("%.3f", t-4000)
			d.faces["train"] = append(d.faces["train"], "1731-dev-"+at+"-Anna-0.62.webp", "1731-dev-"+fmt.Sprintf("%.3f", t-9000)+"-unknown-0.31.webp")
		}
		if c.Detect.Plates {
			add(c.ID, "car", 5000, 25, func(e *devEvent) { e.Plate, e.SubLabel = "M-AB 1234", "Annas Auto" })
			add(c.ID, "car", 50000, 15, func(e *devEvent) { e.Plate = "B-XY 987" })
		}
		if i == 0 {
			add(c.ID, labels[0], 30, 0, func(e *devEvent) { e.End, e.NoSnapshot = nil, true })
			add(c.ID, labels[0], 26*3600, 40, func(e *devEvent) { e.Kept = true })
			d.exports = append(d.exports,
				devExport{ID: c.ID + "_dev001", Camera: c.ID, Name: "Paketbote", Date: t - 7200, State: "done", Size: len(devFile("clip.mp4"))},
				devExport{ID: c.ID + "_dev002", Camera: c.ID, Name: "Nacht", Date: t - 60, State: "running"},
				devExport{ID: c.ID + "_dev003", Camera: c.ID, Name: "Lücke", Date: t - 600, State: "failed"},
			)
		}
	}
	slices.SortFunc(d.events, func(a, b devEvent) int {
		switch {
		case a.Start > b.Start:
			return -1
		case a.Start < b.Start:
			return 1
		}
		return 0
	})
}

// AddEvent puts an event into the stand-in and announces it on the
// WebSocket like Frigate does ("new").
func (f *DevFrigate) AddEvent(id, camera, label, subLabel, plate string, start float64) {
	e := devEvent{ID: id, Camera: camera, Label: label, SubLabel: subLabel, Plate: plate, Start: start, Score: 0.8}
	f.mu.Lock()
	f.data.events = append([]devEvent{e}, f.data.events...)
	f.mu.Unlock()
	after := e.json()
	after["score"], after["top_score"], after["recognized_license_plate"] = e.Score, e.Score, nil
	if plate != "" {
		after["recognized_license_plate"] = []any{plate, 0.9}
	}
	if subLabel != "" {
		after["sub_label"] = []any{subLabel, 0.9}
	}
	payload, _ := json.Marshal(map[string]any{"type": "new", "before": after, "after": after})
	f.Publish("events", string(payload))
}

// Publish sends a message to everything connected to the WebSocket, as
// Frigate does: {"topic", "payload"} with the payload as a string.
func (f *DevFrigate) Publish(topic, payload string) {
	data, _ := json.Marshal(map[string]any{"topic": topic, "payload": payload})
	f.mu.Lock()
	sockets := make([]*websocket.Conn, 0, len(f.data.sockets))
	for conn := range f.data.sockets {
		sockets = append(sockets, conn)
	}
	f.mu.Unlock()
	for _, conn := range sockets {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Write(ctx, websocket.MessageText, data) // a window that went away
		cancel()
	}
}

// Commands returns what came in over the WebSocket (PTZ).
func (f *DevFrigate) Commands() []DevCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.data.commands)
}

// Connected tells how many WebSockets are open.
func (f *DevFrigate) Connected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data.sockets)
}

// SetHours replaces the recordings of a camera (tests of the storage cap).
func (f *DevFrigate) SetHours(camera string, hours []devHour) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.hours == nil {
		f.data.hours = map[string][]devHour{}
	}
	f.data.hours[camera] = hours
}

func writeJSONTo(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the client went away; nothing to do
}

func number(r *http.Request, name string) float64 {
	v, _ := strconv.ParseFloat(r.URL.Query().Get(name), 64) // missing or no number: 0, which means "open"
	return v
}

func csv(r *http.Request, name string) []string {
	value := r.URL.Query().Get(name)
	if value == "" || value == "all" {
		return nil
	}
	return strings.Split(value, ",")
}

// serveData answers the routes of this file; false if the path is none
// of them.
func (f *DevFrigate) serveData(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	ok := func() { writeJSONTo(w, 200, map[string]any{"success": true, "message": "ok"}) }
	notFound := func() { writeJSONTo(w, 404, map[string]any{"success": false, "message": "not found"}) }
	f.mu.Lock()
	d := &f.data
	if d.sockets == nil {
		d.sockets = map[*websocket.Conn]struct{}{}
	}
	defer f.mu.Unlock()
	switch {
	case path == "/ws":
		f.mu.Unlock()
		f.serveSocket(w, r)
		f.mu.Lock()
	case path == "/live/webrtc/api/ws":
		f.mu.Unlock()
		f.serveWebRTC(w, r)
		f.mu.Lock()
	case path == "/api/events" && r.Method == http.MethodGet && f.FailKept && r.URL.Query().Get("favorites") == "1":
		writeJSONTo(w, 500, map[string]any{"success": false, "message": "database is locked"})
	case path == "/api/events" && r.Method == http.MethodGet:
		cameras, labels := csv(r, "cameras"), csv(r, "labels")
		after, before := number(r, "after"), number(r, "before")
		limit := int(number(r, "limit"))
		if limit <= 0 {
			limit = 100
		}
		out := []map[string]any{}
		for _, e := range d.events {
			if (cameras != nil && !slices.Contains(cameras, e.Camera)) || (labels != nil && !slices.Contains(labels, e.Label)) ||
				(after > 0 && e.Start <= after) || (before > 0 && e.Start >= before) || (r.URL.Query().Get("favorites") == "1" && !e.Kept) {
				continue
			}
			if out = append(out, e.json()); len(out) >= limit {
				break
			}
		}
		writeJSONTo(w, 200, out)
	case len(parts) == 3 && parts[0] == "api" && parts[1] == "events":
		at := slices.IndexFunc(d.events, func(e devEvent) bool { return e.ID == parts[2] })
		switch {
		case at < 0:
			notFound()
		case r.Method == http.MethodDelete:
			d.events = slices.Delete(d.events, at, at+1)
			ok()
		default:
			writeJSONTo(w, 200, d.events[at].json())
		}
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "events":
		at := slices.IndexFunc(d.events, func(e devEvent) bool { return e.ID == parts[2] })
		if at < 0 {
			notFound()
			break
		}
		e := d.events[at]
		switch parts[3] {
		case "thumbnail.jpg", "snapshot.jpg":
			if parts[3] == "snapshot.jpg" && e.NoSnapshot {
				notFound()
				break
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(demoPicture(e.Camera + e.Label))
		case "clip.mp4":
			serveBytes(w, r, "video/mp4", devFile("clip.mp4"))
		default:
			notFound()
		}
	case path == "/api/review":
		out := []map[string]any{}
		for _, e := range d.events {
			out = append(out, devReview(e))
		}
		writeJSONTo(w, 200, out)
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "review" && parts[2] == "event":
		at := slices.IndexFunc(d.events, func(e devEvent) bool { return e.ID == parts[3] })
		if at < 0 {
			notFound()
			break
		}
		writeJSONTo(w, 200, devReview(d.events[at]))
	case path == "/api/reviews/viewed" && r.Method == http.MethodPost:
		var body struct {
			IDs      []string `json:"ids"`
			Reviewed *bool    `json:"reviewed"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // a broken body marks nothing
		for i := range d.events {
			if slices.Contains(body.IDs, "r-"+d.events[i].ID) {
				d.events[i].Reviewed = body.Reviewed == nil || *body.Reviewed
			}
		}
		ok()
	case path == "/api/recordings/storage":
		usage := map[string]any{}
		for camera, hours := range d.hours {
			total := 0.0
			for _, h := range hours {
				total += h.MB
			}
			usage[camera] = map[string]any{"bandwidth": 1.2, "usage": total, "usage_percent": 1.0}
		}
		writeJSONTo(w, 200, usage)
	case len(parts) == 6 && parts[0] == "api" && parts[1] == "recordings" && parts[2] == "start" && parts[4] == "end" && r.Method == http.MethodDelete:
		from, _ := strconv.ParseFloat(parts[3], 64)
		to, _ := strconv.ParseFloat(parts[5], 64)
		d.deleted = append(d.deleted, [2]float64{from, to})
		for camera, hours := range d.hours {
			kept := hours[:0]
			for _, h := range hours {
				// What of the hour's recording lies in [from, to) goes.
				lo, hi := max(h.Start, from), min(h.Start+h.Seconds, to)
				if hi > lo && h.Seconds > 0 {
					share := (hi - lo) / h.Seconds
					h.MB -= h.MB * share
					if lo <= h.Start {
						h.Start, h.Seconds = hi, h.Start+h.Seconds-hi
					} else {
						h.Seconds = lo - h.Start
					}
				}
				if h.Seconds > 0.5 {
					kept = append(kept, h)
				}
			}
			d.hours[camera] = kept
		}
		ok()
	case len(parts) == 4 && parts[0] == "api" && parts[2] == "recordings" && parts[3] == "summary":
		zone := time.UTC
		if name := r.URL.Query().Get("timezone"); name != "" {
			if loaded, err := time.LoadLocation(name); err == nil {
				zone = loaded
			}
		}
		days := map[string]map[string]any{}
		order := []string{}
		for _, h := range d.hours[parts[1]] {
			at := time.Unix(int64(h.Start), 0).In(zone)
			day := at.Format("2006-01-02")
			events := 0
			for _, e := range d.events {
				if e.Camera == parts[1] && e.Start >= h.Start && e.Start < h.Start+3600 {
					events++
				}
			}
			if days[day] == nil {
				days[day] = map[string]any{"day": day, "events": 0, "hours": []map[string]any{}}
				order = append(order, day)
			}
			days[day]["hours"] = append(days[day]["hours"].([]map[string]any), map[string]any{
				"hour": at.Format("15"), "events": events, "motion": int(h.Seconds / 9), "objects": events * 30, "duration": int(h.Seconds)})
		}
		out := []map[string]any{}
		for _, day := range order {
			out = append(out, days[day])
		}
		writeJSONTo(w, 200, out)
	case len(parts) == 3 && parts[0] == "api" && parts[2] == "recordings":
		after, before := number(r, "after"), number(r, "before")
		out := []map[string]any{}
		for _, h := range d.hours[parts[1]] {
			for s := h.Start; s < h.Start+h.Seconds; s += 10 {
				end := min(s+10, h.Start+h.Seconds)
				if end < after || s > before {
					continue
				}
				motion, objects := 0, 0
				if int(s/10)%9 < 3 {
					motion = 40 + int(s/10)%60
				}
				for _, e := range d.events {
					if e.Camera == parts[1] && e.Start < end && (e.End == nil || *e.End > s) && e.Start > s-600 {
						objects, motion = 1, max(motion, 80)
					}
				}
				out = append(out, map[string]any{"id": fmt.Sprintf("%.0f-%s", s, parts[1]), "start_time": s, "end_time": end,
					"segment_size": 1.25, "motion": motion, "objects": objects, "duration": end - s})
			}
		}
		writeJSONTo(w, 200, out)
	case len(parts) == 5 && parts[0] == "api" && parts[2] == "recordings" && strings.HasPrefix(parts[4], "snapshot."):
		at, _ := strconv.ParseFloat(parts[3], 64)
		inside := false
		for _, h := range d.hours[parts[1]] {
			inside = inside || (at >= h.Start && at < h.Start+h.Seconds)
		}
		if !inside {
			notFound()
			break
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(demoPicture(parts[1] + parts[3][:min(len(parts[3]), 7)]))
	case len(parts) == 7 && parts[0] == "vod" && parts[2] == "start" && parts[4] == "end":
		if _, known := d.hours[parts[1]]; !known {
			http.Error(w, "no recordings", http.StatusNotFound)
			break
		}
		switch file := parts[6]; {
		case file == "master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=200000,RESOLUTION=320x180,CODECS=\"avc1.42c00d\"\nindex-v1.m3u8\n")
		case file == "index-v1.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write(devFile("index-v1.m3u8"))
		case file == "init-v1.mp4", file == "seg-0-v1.m4s", file == "seg-1-v1.m4s":
			serveBytes(w, r, "video/mp4", devFile(file))
		default:
			http.Error(w, "no such file", http.StatusNotFound)
		}
	case len(parts) == 7 && parts[0] == "api" && parts[1] == "export" && parts[3] == "start" && parts[5] == "end" && r.Method == http.MethodPost:
		from, _ := strconv.ParseFloat(parts[4], 64)
		to, _ := strconv.ParseFloat(parts[6], 64)
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // a broken body leaves the name empty
		recorded := false
		for _, h := range d.hours[parts[2]] {
			recorded = recorded || (h.Start < to && h.Start+h.Seconds > from)
		}
		if !recorded {
			writeJSONTo(w, 400, map[string]any{"success": false, "message": "No recordings found for time range"})
			break
		}
		d.nextID++
		id := fmt.Sprintf("%s_new%03d", parts[2], d.nextID)
		// The stand-in "finishes" an export when it is next listed.
		d.exports = append(d.exports, devExport{ID: id, Camera: parts[2], Name: body.Name, Date: float64(time.Now().Unix()), State: "queued", Size: len(devFile("clip.mp4"))})
		writeJSONTo(w, 202, map[string]any{"success": true, "message": "Export queued.", "export_id": id, "status": "queued"})
	case path == "/api/exports":
		out := []map[string]any{}
		for i, e := range d.exports {
			switch e.State {
			case "queued":
				d.exports[i].State = "started"
				continue
			case "started":
				d.exports[i].State, e.State = "done", "done"
			case "failed":
				continue
			}
			out = append(out, map[string]any{"id": e.ID, "camera": e.Camera, "name": e.Name, "date": e.Date,
				"video_path": "/media/frigate/exports/" + e.ID + ".mp4", "thumb_path": "/media/frigate/clips/export/" + e.ID + ".webp", "in_progress": e.State == "running"})
		}
		writeJSONTo(w, 200, out)
	case path == "/api/jobs/export":
		out := []map[string]any{}
		for _, e := range d.exports {
			if e.State == "queued" || e.State == "started" {
				out = append(out, devJob(e))
			}
		}
		writeJSONTo(w, 200, out)
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "jobs" && parts[2] == "export":
		at := slices.IndexFunc(d.exports, func(e devExport) bool { return e.ID == parts[3] })
		if at < 0 {
			notFound()
			break
		}
		writeJSONTo(w, 200, devJob(d.exports[at]))
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "export" && parts[3] == "rename" && r.Method == http.MethodPatch:
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // a broken body leaves the name empty
		at := slices.IndexFunc(d.exports, func(e devExport) bool { return e.ID == parts[2] })
		if at < 0 {
			notFound()
			break
		}
		d.exports[at].Name = body.Name
		ok()
	case path == "/api/exports/delete" && r.Method == http.MethodPost:
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // a broken body deletes nothing
		d.exports = slices.DeleteFunc(d.exports, func(e devExport) bool { return slices.Contains(body.IDs, e.ID) })
		ok()
	case len(parts) == 2 && parts[0] == "exports":
		at := slices.IndexFunc(d.exports, func(e devExport) bool { return e.ID+".mp4" == parts[1] && e.State == "done" })
		if at < 0 {
			http.NotFound(w, r)
			break
		}
		serveBytes(w, r, "video/mp4", devFile("clip.mp4"))
	case len(parts) == 3 && parts[0] == "clips" && parts[1] == "export":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(demoPicture(parts[2]))
	case len(parts) == 4 && parts[0] == "api" && parts[2] == "ptz" && parts[3] == "info":
		if parts[1] != "garten" {
			writeJSONTo(w, 200, map[string]any{})
			break
		}
		writeJSONTo(w, 200, map[string]any{"name": parts[1], "features": []string{"pt", "zoom"}, "presets": []string{"home", "tor", "terrasse"}, "profiles": []any{}})
	case path == "/api/faces" && r.Method == http.MethodGet:
		writeJSONTo(w, 200, d.faces)
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "faces" && r.Method != http.MethodGet:
		name := parts[2]
		switch parts[3] {
		case "create":
			if d.faces[name] == nil {
				d.faces[name] = []string{}
			}
			ok()
		case "register":
			body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
			// A picture "without a face": the stand-in refuses tiny uploads.
			if len(body) < 400 {
				writeJSONTo(w, 400, map[string]any{"success": false, "message": "No face was detected."})
				break
			}
			d.nextID++
			d.faces[name] = append(d.faces[name], fmt.Sprintf("%s-%d.webp", name, d.nextID))
			ok()
		case "delete":
			var body struct {
				IDs []string `json:"ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body) // a broken body deletes nothing
			d.faces[name] = slices.DeleteFunc(d.faces[name], func(file string) bool { return slices.Contains(body.IDs, file) })
			if name != "train" && len(d.faces[name]) == 0 {
				delete(d.faces, name)
			}
			ok()
		case "rename":
			var body struct {
				NewName string `json:"new_name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body) // a broken body leaves the name empty
			if d.faces[name] == nil || body.NewName == "" {
				writeJSONTo(w, 400, map[string]any{"success": false, "message": "Error renaming face."})
				break
			}
			d.faces[body.NewName] = d.faces[name]
			delete(d.faces, name)
			ok()
		default:
			notFound()
		}
	case len(parts) == 5 && parts[0] == "api" && parts[1] == "faces" && parts[2] == "train" && parts[4] == "classify":
		var body struct {
			File string `json:"training_file"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // a broken body names no file
		if !slices.Contains(d.faces["train"], body.File) {
			notFound()
			break
		}
		d.faces["train"] = slices.DeleteFunc(d.faces["train"], func(file string) bool { return file == body.File })
		d.nextID++
		d.faces[parts[3]] = append(d.faces[parts[3]], fmt.Sprintf("%s-%d.webp", parts[3], d.nextID))
		ok()
	case len(parts) == 4 && parts[0] == "clips" && parts[1] == "faces":
		if !slices.Contains(d.faces[parts[2]], parts[3]) {
			http.NotFound(w, r)
			break
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(demoPicture(parts[2] + parts[3]))
	case path == "/api/recognized_license_plates":
		writeJSONTo(w, 200, d.plates)
	default:
		return false
	}
	return true
}

func devReview(e devEvent) map[string]any {
	end := e.Start + 30
	if e.End != nil {
		end = *e.End
	}
	return map[string]any{"id": "r-" + e.ID, "camera": e.Camera, "start_time": e.Start - 2, "end_time": end, "severity": "alert",
		"has_been_reviewed": e.Reviewed, "thumb_path": "", "data": map[string]any{"detections": []string{e.ID}, "objects": []string{e.Label}}}
}

func devJob(e devExport) map[string]any {
	status := map[string]string{"queued": "queued", "started": "running", "running": "running", "done": "success", "failed": "failed"}[e.State]
	return map[string]any{"id": e.ID, "job_type": "export", "status": status, "camera": e.Camera, "name": e.Name, "progress_percent": 40.0}
}

// serveBytes answers a file with Range support, as nginx does.
func serveBytes(w http.ResponseWriter, r *http.Request, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// serveSocket is Frigate's /ws: it answers "onConnect" with the state of
// every camera and records every other message as a command.
func (f *DevFrigate) serveSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	f.mu.Lock()
	f.data.sockets[conn] = struct{}{}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.data.sockets, conn)
		f.mu.Unlock()
	}()
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var message struct {
			Topic   string `json:"topic"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		if message.Topic != "onConnect" {
			f.mu.Lock()
			f.data.commands = append(f.data.commands, DevCommand{message.Topic, message.Payload})
			f.mu.Unlock()
			continue
		}
		f.mu.Lock()
		cameras := map[string]float64{}
		for id, fps := range f.Cameras {
			cameras[id] = fps
		}
		var going []devEvent
		for _, e := range f.data.events {
			if e.End == nil {
				going = append(going, e)
			}
		}
		f.mu.Unlock()
		for id, fps := range cameras {
			motion := "OFF"
			if fps > 0 && slices.ContainsFunc(going, func(e devEvent) bool { return e.Camera == id }) {
				motion = "ON"
			}
			f.Publish(id+"/motion", motion)
		}
		for _, e := range going {
			f.Publish(e.Camera+"/"+e.Label, "1")
		}
	}
}

// serveWebRTC is go2rtc's signalling: it answers an offer with an answer
// that no browser can use, so the page shows "cannot connect" instead of
// hanging.
func (f *DevFrigate) serveWebRTC(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var message struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &message) == nil && message.Type == "webrtc/offer" {
			answer, _ := json.Marshal(map[string]string{"type": "error", "value": "development mode has no camera to talk to (src " + r.URL.Query().Get("src") + ")"})
			if conn.Write(r.Context(), websocket.MessageText, answer) != nil {
				return
			}
		}
	}
}

// devExportRecords are the records of the example exports (Seed): they
// belong to no account, so administrators see them.
func devExportRecords(cameras []Camera) map[string]exportRecord {
	records := map[string]exportRecord{}
	for _, c := range cameras {
		if !c.Enabled {
			continue
		}
		now := float64(time.Now().Unix())
		records[c.ID+"_dev001"] = exportRecord{From: now - 7500, To: now - 7380}
		records[c.ID+"_dev002"] = exportRecord{From: now - 3600, To: now - 1800}
		records[c.ID+"_dev003"] = exportRecord{From: now - 24*3600, To: now - 23*3600}
		break
	}
	return records
}
