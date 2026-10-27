package api

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// pendingActivity is filled in while a request runs: requireUser knows the
// account and route, handlers may name what they acted on.
type pendingActivity struct {
	user    string
	via     string
	action  string
	targets []string
}

type activityKey struct{}

// activityQuiet are changes too frequent or too private to be worth an entry:
// upload chunks, previews and searches, personal UI state and chats.
var activityQuiet = []string{
	"PATCH /api/v1/files/uploads/{id}",
	"POST /api/v1/files/uploads/{id}/verify",
	"POST /api/v1/files/compare",
	"POST /api/v1/files/duplicates",
	"POST /api/v1/files/inspect",
	"POST /api/v1/files/rename-preview",
	"POST /api/v1/files/search",
	"POST /api/v1/storage/smart/refresh",
	"POST /api/v1/files/usage",
	"POST /api/v1/files/url-download/probe",
	"POST /api/v1/assistant/knowledge/search",
	"POST /api/v1/assistant/automations/preview",
	"POST /api/v1/assistant/probe",
	"PUT /api/v1/preferences/{kind}",
	"PUT /api/v1/background",
	"DELETE /api/v1/background",
	"POST /api/v1/notifications/dismiss",
	"DELETE /api/v1/plugins/progress/{id}",
	"POST /api/v1/storage/volumes/{name}/growth/preview",
	"POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/preview",
	"POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/preview",
	"POST /api/v1/storage/import/preview",
	"POST /api/v1/network/diagnostics",
	"POST /api/v1/backups/smb/discover",
	"POST /api/v1/homeassistant/check",
	"POST /api/v1/files/archive",
}

// activityReads are reads that open a console or take sensitive data along.
var activityReads = []string{
	"GET /api/v1/terminal/shell",
	"GET /api/v1/docker/containers/{id}/console",
	"GET /api/v1/vms/{name}/serial",
	"GET /api/v1/vms/{name}/screen",
	"GET /api/v1/lxc/{name}/console",
	"GET /api/v1/config-backup/download",
	"GET /api/v1/diagnostics/bundle",
}

var activityParam = regexp.MustCompile(`\{([A-Za-z]+)\.{0,3}\}`)

// activityLog wraps the API so that every change and console is recorded
// once the handler is done.
func (s *Server) activityLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Activity == nil || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		rec := &pendingActivity{}
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), activityKey{}, rec)))
		if rec.user == "" || rec.action == "" {
			return
		}
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		s.opts.Activity.Record(activity.Entry{
			User:   rec.user,
			Via:    rec.via,
			IP:     remoteIP(r),
			Action: rec.action,
			Target: strings.Join(rec.targets, ", "),
			Status: status,
			OK:     status < 400 || status == http.StatusSwitchingProtocols,
		})
	})
}

// noteActivity is called by requireUser once the account is known.
func noteActivity(r *http.Request, user auth.User) {
	rec, ok := r.Context().Value(activityKey{}).(*pendingActivity)
	if !ok || r.Pattern == "" {
		return
	}
	action := r.Pattern
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if !contains(activityReads, action) {
			return
		}
	} else if contains(activityQuiet, action) || strings.HasPrefix(action, "POST /api/v1/assistant/chats") || strings.HasPrefix(action, "PUT /api/v1/assistant/chats") || strings.HasPrefix(action, "DELETE /api/v1/assistant/chats") {
		return
	}
	rec.user = user.Name
	rec.action = action
	rec.via = activity.ViaWeb
	if _, ok := r.Context().Value(apiTokenIdentity{}).(string); ok {
		rec.via = activity.ViaToken
	} else if _, ok := r.Context().Value(agentIdentity{}).(string); ok {
		rec.via = activity.ViaAgent
	}
	for _, m := range activityParam.FindAllStringSubmatch(action, -1) {
		// One-time tokens in a path are secrets, never names.
		if v := r.PathValue(m[1]); v != "" && m[1] != "token" {
			rec.targets = append(rec.targets, v)
		}
	}
}

// noteTarget names what a request acted on when the path does not say it,
// such as the files of a delete. Only names belong here, never contents.
func noteTarget(r *http.Request, targets ...string) {
	rec, ok := r.Context().Value(activityKey{}).(*pendingActivity)
	if !ok {
		return
	}
	const most = 20
	for _, t := range targets {
		if t == "" {
			continue
		}
		if len(rec.targets) >= most {
			rec.targets = append(rec.targets[:most], "…")
			return
		}
		rec.targets = append(rec.targets, t)
	}
}

// filePlace names a file as "share/path" for the log.
func filePlace(root, p string) string {
	if p == "" {
		return root
	}
	return path.Join(root, p)
}

func filePlaces(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filePlace(root, p))
	}
	return out
}

// urlPlace keeps only the server of a download link: queries and user
// information often carry tokens.
func urlPlace(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// recordEvent writes an entry outside a logged-in request, such as a login.
func (s *Server) recordEvent(r *http.Request, user, action, target string, ok bool) {
	if s.opts.Activity == nil {
		return
	}
	s.opts.Activity.Record(activity.Entry{User: user, Via: activity.ViaWeb, IP: remoteIP(r), Action: action, Target: target, OK: ok})
}

// statusWriter remembers the status code and still lets WebSockets take
// over the connection.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil && w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if s.opts.Activity == nil {
		featureUnavailable(w)
		return
	}
	q := r.URL.Query()
	f := activity.Filter{Days: 7, Limit: 200, User: q.Get("user"), Text: q.Get("q"), Failed: q.Get("failed") == "1"}
	if v := q.Get("days"); v != "" {
		f.Days, _ = strconv.Atoi(v)
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	if v := q.Get("before"); v != "" {
		before, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "before is not a time")
			return
		}
		f.Before = before
	}
	format := q.Get("format")
	if format == "csv" || format == "json" {
		f.Limit = 50000
		f.Before = time.Time{}
	} else if f.Limit > 1000 {
		f.Limit = 1000
	}
	entries, more, stored, err := s.opts.Activity.Query(r.Context(), f)
	if err != nil {
		featureError(w, err)
		return
	}
	stamp := time.Now().UTC().Format("2006-01-02")
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="titanos-activity-`+stamp+`.csv"`)
		out := csv.NewWriter(w)
		out.Write([]string{"time", "user", "via", "ip", "action", "target", "status", "ok"})
		for _, e := range entries {
			out.Write([]string{e.At.Format(time.RFC3339), csvSafe(e.User), e.Via, e.IP, e.Action, csvSafe(e.Target), strconv.Itoa(e.Status), strconv.FormatBool(e.OK)})
		}
		out.Flush()
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="titanos-activity-`+stamp+`.json"`)
		json.NewEncoder(w).Encode(entries)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "more": more, "stored": stored, "status": s.opts.Activity.Status()})
	}
}

// csvSafe keeps names from being run as formulas when the export is opened
// in a spreadsheet.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (s *Server) handleActivitySettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Activity == nil {
		featureUnavailable(w)
		return
	}
	var req activity.Settings
	if !readJSON(w, r, &req) {
		return
	}
	// Turning the log off is the last thing it records.
	if was := s.opts.Activity.Status().Settings; was.Enabled && !req.Enabled {
		if rec, ok := r.Context().Value(activityKey{}).(*pendingActivity); ok && rec.user != "" {
			s.opts.Activity.Record(activity.Entry{User: rec.user, Via: rec.via, IP: remoteIP(r), Action: rec.action, Target: "off", Status: http.StatusOK, OK: true})
			rec.action = ""
		}
	}
	if err := s.opts.Activity.Set(req); err != nil {
		featureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Activity.Status())
}
