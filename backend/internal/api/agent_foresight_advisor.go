package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/advisor"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
)

// The Hardware-Berater runs once a night at its time (03:30 unless set,
// the agent's default thinking time) and makes up a missed night during the
// day. It waits while a disk of the agent's volume, or of the volume with
// the SMART history, sleeps: it reads nothing that would wake a disk. The
// report lies in advisor.json of the agent's shared folder
// (.titanos/agent on the agent's volume), never on the stick.

const advisorFile = "advisor.json"

// advisorOffer is one result of the web search for a replacement; it is
// foreign content and never checked.
type advisorOffer struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Price string `json:"price,omitempty"`
}

// advisorSaved is the kept state of the adviser.
type advisorSaved struct {
	Report advisor.Report `json:"report"`
	// Offers per disk, only with "Ersatz mit Preis".
	Offers map[string][]advisorOffer `json:"offers,omitempty"`
	// History: the SMART history was read (else only today's values).
	History     bool      `json:"history"`
	LastRun     time.Time `json:"lastRun,omitzero"`
	LastMonthly string    `json:"lastMonthly,omitempty"`
	// Volumes are the hourly samples of volume usage, kept for the growth.
	Volumes  []monitoring.Sample `json:"volumes,omitempty"`
	Measured map[string]uint64   `json:"measured,omitempty"`
}

// advisorLoad reads the kept state once, when the agent's volume is awake.
func (s *Server) advisorLoad() {
	st := s.foresight()
	st.mu.Lock()
	loaded := st.loaded
	st.mu.Unlock()
	if loaded || s.advisorStoreAsleep() {
		return
	}
	var saved advisorSaved
	err := s.agentStore().ReadShared(advisorFile, &saved)
	if err != nil && !errors.Is(err, agent.ErrNotFound) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loaded = true
	st.saved = saved
	if len(st.vols) == 0 {
		st.vols = saved.Volumes
	}
	for k, v := range saved.Measured {
		if _, ok := st.measured[k]; !ok {
			st.measured[k] = v
		}
	}
}

// advisorReport is the last report, if there is one.
func (s *Server) advisorReport() (advisorSaved, bool) {
	s.advisorLoad()
	st := s.foresight()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.saved, !st.saved.LastRun.IsZero()
}

// advisorDue: the time of today has come and there was no run since; a
// missed night is made up while the day lasts (like the thinking).
func advisorDue(at string, last, now time.Time) bool {
	t, err := time.ParseInLocation("15:04", at, now.Location())
	if err != nil {
		return false
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	return !now.Before(due) && last.Before(due) && now.Sub(due) < 20*time.Hour
}

func (s *Server) advisorNightly(now time.Time, a agentAdvisor) {
	st := s.foresight()
	st.mu.Lock()
	if st.running || now.Sub(st.tried) < 10*time.Minute {
		st.mu.Unlock()
		return
	}
	last, loaded := st.saved.LastRun, st.loaded
	st.mu.Unlock()
	if loaded && !advisorDue(a.at(), last, now) || !loaded && !advisorDue(a.at(), time.Time{}, now) {
		return
	}
	st.mu.Lock()
	st.tried = now
	st.mu.Unlock()
	// Later, when the disks are awake anyway.
	if s.advisorStoreAsleep() || s.opts.SmartHistory != nil && s.volumeAsleep(s.smartHistoryVolume()) {
		return
	}
	s.advisorLoad()
	st.mu.Lock()
	if !st.loaded || !advisorDue(a.at(), st.saved.LastRun, now) || st.running {
		st.mu.Unlock()
		return
	}
	st.running = true
	st.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), 5*time.Minute)
		defer cancel()
		s.runAdvisor(ctx, a, now, true)
	}()
}

// runAdvisor judges now and keeps the report. nightly runs also send the
// monthly report.
func (s *Server) runAdvisor(ctx context.Context, a agentAdvisor, now time.Time, nightly bool) advisorSaved {
	st := s.foresight()
	defer func() {
		st.mu.Lock()
		st.running = false
		st.mu.Unlock()
	}()
	s.advisorLoad()
	history := s.opts.SmartHistory != nil && !s.volumeAsleep(s.smartHistoryVolume())
	in := s.advisorInput(ctx, a, now, history)
	report := advisor.Judge(in)
	var offers map[string][]advisorOffer
	if a.Prices && s.agentWebAllowed() {
		offers = s.advisorOffers(ctx, report)
	}
	st.mu.Lock()
	saved := st.saved
	saved.Report, saved.Offers, saved.History, saved.LastRun = report, offers, history || st.input != nil, now
	saved.Volumes = append([]monitoring.Sample{}, st.vols...)
	saved.Measured = maps.Clone(st.measured)
	st.mu.Unlock()
	if nightly && a.Monthly && saved.LastMonthly != now.Format("2006-01") && s.opts.Notify != nil {
		if err := s.opts.Notify.SendReport("Hardware-Berater", advisorText(report)); err != nil {
			slog.Info("hardware adviser report not sent", "err", err)
		} else {
			saved.LastMonthly = now.Format("2006-01")
		}
	}
	if !s.advisorStoreAsleep() {
		if err := s.agentStore().WriteShared(advisorFile, saved); err != nil {
			slog.Warn("hardware adviser report not kept", "err", err)
		}
	}
	st.mu.Lock()
	st.saved = saved
	st.mu.Unlock()
	return saved
}

// agentWebAllowed: the agent's policy lets it search the web.
func (s *Server) agentWebAllowed() bool {
	return s.agentSettings().Policy.Mode("web_search", agent.AreaWeb, agent.KindRead) != agent.ModeDeny
}

var priceRe = regexp.MustCompile(`(\d{1,4}(?:[.,]\d{2})?)\s?€|€\s?(\d{1,4}(?:[.,]\d{2})?)|EUR\s?(\d{1,4}(?:[.,]\d{2})?)`)

// advisorOffers searches a replacement for each disk that will probably
// fail. The query holds only model and size, never a serial number, name
// or path.
func (s *Server) advisorOffers(ctx context.Context, report advisor.Report) map[string][]advisorOffer {
	out := map[string][]advisorOffer{}
	web := s.agentWeb()
	for _, f := range report.Findings {
		if f.Area != advisor.AreaDisk || f.Level != advisor.LevelFail || f.Model == "" {
			continue
		}
		query := replacementQuery(f)
		results, err := web.Search(ctx, s.opts.Version, query)
		if err != nil {
			continue
		}
		for _, r := range results {
			offer := advisorOffer{Title: r.Title, URL: r.URL}
			if m := priceRe.FindStringSubmatch(r.Title + " " + r.Snippet); m != nil {
				offer.Price = strings.TrimSpace(m[1] + m[2] + m[3])
			}
			out[f.Subject] = append(out[f.Subject], offer)
			if len(out[f.Subject]) == 3 {
				break
			}
		}
	}
	return out
}

func replacementQuery(f advisor.Finding) string {
	kind := "Festplatte"
	switch f.Kind {
	case "ssd":
		kind = "SSD"
	case "nvme":
		kind = "NVMe SSD"
	}
	size := ""
	if f.SizeBytes > 0 {
		size = fmt.Sprintf(" %.0f TB", float64(f.SizeBytes)/1e12)
		if f.SizeBytes < 1e12 {
			size = fmt.Sprintf(" %.0f GB", float64(f.SizeBytes)/1e9)
		}
	}
	return fmt.Sprintf("%s%s %s Preis", f.Model, size, kind)
}

// advisorText is the monthly report (German, like the weekly report).
func advisorText(r advisor.Report) string {
	var lines []string
	for _, f := range r.Findings {
		if f.Level == advisor.LevelOK {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s %s: %s (%s)", f.Area, f.Subject, f.Code, f.Level))
	}
	if len(lines) == 0 {
		return "Der Hardware-Berater hat diesen Monat nichts Auffälliges gefunden."
	}
	return "Der Hardware-Berater rät zu einem Blick auf:\n" + strings.Join(lines, "\n")
}

type advisorView struct {
	Settings agentAdvisor  `json:"settings"`
	Saved    *advisorSaved `json:"saved,omitempty"`
	Running  bool          `json:"running"`
	Web      bool          `json:"web"`
}

func (s *Server) advisorView() advisorView {
	a := s.agentSettings().Features.Advisor
	v := advisorView{Settings: a.view(), Web: s.agentWebAllowed()}
	if saved, ok := s.advisorReport(); ok {
		saved.Volumes, saved.Measured = nil, nil
		v.Saved = &saved
	}
	st := s.foresight()
	st.mu.Lock()
	v.Running = st.running
	st.mu.Unlock()
	return v
}

func (s *Server) handleAdvisor(w http.ResponseWriter, r *http.Request) {
	if !s.agentSettings().Features.Advisor.Enabled {
		featureOff(w)
		return
	}
	writeJSON(w, http.StatusOK, s.advisorView())
}

var errAdvisorRunning = errors.New("the hardware adviser runs already")

// advisorRunNow judges now, with the values there are without waking a
// disk.
func (s *Server) advisorRunNow(ctx context.Context, a agentAdvisor) (advisorSaved, error) {
	st := s.foresight()
	st.mu.Lock()
	if st.running {
		st.mu.Unlock()
		return advisorSaved{}, errAdvisorRunning
	}
	st.running = true
	st.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return s.runAdvisor(ctx, a, time.Now().In(s.agentLocation()), false), nil
}

func (s *Server) handleAdvisorRun(w http.ResponseWriter, r *http.Request) {
	a := s.agentSettings().Features.Advisor
	if !a.Enabled {
		featureOff(w)
		return
	}
	if _, err := s.advisorRunNow(r.Context(), a); err != nil {
		writeError(w, http.StatusConflict, "advisor_running", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.advisorView())
}

// advisorStoreAsleep: reading or writing the adviser's file would wake a
// disk (or there is no agent volume). The test store never sleeps.
func (s *Server) advisorStoreAsleep() bool {
	if s.agentOpen != nil {
		return false
	}
	return s.agentVolumeAsleep()
}
