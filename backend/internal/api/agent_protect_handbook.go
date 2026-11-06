package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"github.com/ra5on/TitanOS/backend/internal/protect"
)

// Notfallhandbuch (PLAN 54-S.3): the build of the NAS and "Was tun, wenn …"
// from what titand knows without waking a disk (storage status from lsblk
// and mdadm, shares and backup jobs from RAM, apps and VMs from the NAS map
// of the night). Only names, levels, models, serial numbers and addresses
// go in; never passwords, keys or tokens. Made again at night (after the
// NAS map, 03:20–03:59) when something changed, and on request.

type handbookDoc struct {
	Hash  string        `json:"hash"`
	Made  time.Time     `json:"made"`
	Facts protect.Facts `json:"facts"`
}

// handbookFacts collects the facts. Contacts and probes only when chosen.
func (s *Server) handbookFacts(ctx context.Context, h agentHandbook) protect.Facts {
	f := protect.Facts{Version: s.opts.Version, Volumes: []protect.VolumeFact{}, Shares: []protect.NamedFact{}}
	if s.opts.Monitor != nil {
		f.Host = s.opts.Monitor.Info().Hostname
	}
	if f.Host == "" {
		f.Host = "TitanOS"
	}
	if s.opts.Storage != nil {
		if st, err := s.opts.Storage.Status(ctx); err == nil {
			disks := map[string]protect.DiskFact{}
			for _, d := range st.Disks {
				disks[d.Name] = protect.DiskFact{Model: strings.TrimSpace(d.Model), Serial: d.Serial, Size: d.SizeBytes, Kind: d.Kind}
				if d.Role == "boot" || d.Role == "stick" {
					f.Stick = strings.TrimSpace(d.Model + " " + gbText(d.SizeBytes))
				}
			}
			for _, v := range st.Volumes {
				vf := protect.VolumeFact{Name: v.Name, Level: v.Level, Filesystem: v.Filesystem, Size: v.SizeBytes, Encrypted: v.Encryption != nil, Disks: []protect.DiskFact{}}
				for _, m := range v.Members {
					d := disks[m.Disk]
					d.State = m.State
					vf.Disks = append(vf.Disks, d)
				}
				slices.SortFunc(vf.Disks, func(a, b protect.DiskFact) int { return strings.Compare(a.Serial, b.Serial) })
				f.Volumes = append(f.Volumes, vf)
			}
		}
	}
	if s.opts.Shares != nil {
		for _, sh := range s.opts.Shares.List() {
			f.Shares = append(f.Shares, protect.NamedFact{Name: sh.Name, Detail: sh.Volume})
		}
	}
	if m, ok := s.agentMap(); ok {
		for _, a := range m.Apps {
			f.Apps = append(f.Apps, protect.NamedFact{Name: a.Name, Detail: a.Address})
		}
		for _, v := range m.VMs {
			f.VMs = append(f.VMs, protect.NamedFact{Name: v.Name, Detail: v.Detail})
		}
		for _, c := range m.LXC {
			f.VMs = append(f.VMs, protect.NamedFact{Name: c.Name, Detail: strings.TrimSpace("LXC " + c.Detail)})
		}
	}
	f.Network = handbookNetwork()
	if ts := s.opts.Tailscale; ts != nil {
		if st := ts.Status(); st.Active {
			f.Remote = append(f.Remote, protect.NamedFact{Name: "Tailscale", Detail: strings.TrimSpace(st.Name + " " + strings.Join(st.IPs, ", "))})
		}
	}
	if nb := s.opts.Netbird; nb != nil {
		if st := nb.Status(); st.Active {
			f.Remote = append(f.Remote, protect.NamedFact{Name: "NetBird", Detail: st.FQDN})
		}
	}
	if wg := s.opts.WireGuard; wg != nil {
		if st := wg.Status(); st.Enabled {
			f.Remote = append(f.Remote, protect.NamedFact{Name: "WireGuard", Detail: strings.TrimSpace(fmt.Sprintf("%s:%d", st.Endpoint, st.Port))})
		}
	}
	if b := s.opts.Backup; b != nil {
		for _, j := range b.List() {
			f.Backups = append(f.Backups, protect.NamedFact{Name: j.Job.ID, Detail: backupWhere(j.Job.Destination)})
			if h.Probes && j.State.Probe != nil {
				p := j.State.Probe
				detail := fmt.Sprintf("%s: %d Dateien geprüft", p.At.In(s.agentLocation()).Format("02.01.2006"), p.Files)
				if p.Error != "" {
					detail = fmt.Sprintf("%s: fehlgeschlagen", p.At.In(s.agentLocation()).Format("02.01.2006"))
				}
				f.Probes = append(f.Probes, protect.NamedFact{Name: j.Job.ID, Detail: detail})
			}
		}
	}
	if h.WithContacts {
		f.Contacts = h.Contacts
	}
	return f
}

func gbText(b uint64) string {
	if b == 0 {
		return ""
	}
	return fmt.Sprintf("%.0f GB", float64(b)/1e9)
}

// backupWhere names a backup's destination without any secret: no keys,
// no passwords, no user info of URLs.
func backupWhere(d backup.Destination) string {
	switch {
	case d.Volume != "":
		return strings.TrimSpace(d.Type + ": " + d.Volume + "/" + strings.TrimPrefix(d.Folder, "/"))
	case d.DeviceName != "":
		return d.Type + ": " + d.DeviceName
	case d.Host != "":
		return d.Type + ": " + d.Host + ":" + d.Path
	case d.Endpoint != "":
		return d.Type + ": " + d.Endpoint + "/" + d.Bucket
	case d.URL != "":
		if u, err := url.Parse(d.URL); err == nil {
			u.User = nil
			u.RawQuery = ""
			return d.Type + ": " + u.String()
		}
		return d.Type
	}
	return d.Type
}

// handbookNetwork lists the addresses of the NAS (from the kernel, no disk).
func handbookNetwork() []protect.NamedFact {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []protect.NamedFact
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 || strings.HasPrefix(ifc.Name, "veth") || strings.HasPrefix(ifc.Name, "docker") || strings.HasPrefix(ifc.Name, "br-") {
			continue
		}
		addrs, _ := ifc.Addrs()
		var list []string
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() {
				list = append(list, ipn.String())
			}
		}
		if len(list) > 0 {
			out = append(out, protect.NamedFact{Name: ifc.Name, Detail: strings.Join(list, ", ")})
		}
	}
	return out
}

func (s *Server) handbookStored() (handbookDoc, bool) {
	var doc handbookDoc
	if err := s.agentStore().ReadShared(handbookFile, &doc); err != nil {
		return doc, false
	}
	return doc, true
}

// handbookRefresh makes the handbook when its facts changed (or force).
func (s *Server) handbookRefresh(ctx context.Context, force bool) (handbookDoc, error) {
	h := s.agentSettings().Features.Handbook
	facts := s.handbookFacts(ctx, h)
	hash := facts.Hash()
	old, ok := s.handbookStored()
	if ok && old.Hash == hash && !force {
		return old, nil
	}
	doc := handbookDoc{Hash: hash, Made: s.protect().now().UTC(), Facts: facts}
	if err := s.agentStore().WriteShared(handbookFile, doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// handbookCurrent is the stored handbook, made now if there is none.
func (s *Server) handbookCurrent(ctx context.Context) (handbookDoc, error) {
	if doc, ok := s.handbookStored(); ok {
		return doc, nil
	}
	return s.handbookRefresh(ctx, false)
}

func (s *Server) handbookTick(h agentHandbook, now time.Time) {
	if !h.Enabled || now.Hour() != 3 || now.Minute() < 20 {
		return
	}
	ps := s.protect()
	ps.mu.Lock()
	if now.Sub(ps.handbookTried) < 20*time.Hour {
		ps.mu.Unlock()
		return
	}
	ps.handbookTried = now
	ps.mu.Unlock()
	if s.agentVolumeAsleep() {
		return
	}
	protectGo("handbook", func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), 2*time.Minute)
		defer cancel()
		if _, err := s.handbookRefresh(ctx, false); err != nil {
			slog.Warn("emergency handbook not made", "err", err)
		}
	})
}

func handbookLang(r *http.Request) bool { return r.URL.Query().Get("lang") != "en" }

func (s *Server) handbookGate(w http.ResponseWriter, r *http.Request) bool {
	user := currentUser(r)
	if !s.agentSettings().Features.Handbook.Enabled {
		writeError(w, http.StatusNotFound, "feature_off", "the emergency handbook is not switched on")
		return false
	}
	if !s.handbookAllowed(user) {
		writeError(w, http.StatusForbidden, "forbidden", "only owners, administrators and a granted emergency access")
		return false
	}
	if !user.Role.CanAdminister() {
		s.emergencyRecord(user.Name, "emergency handbook read", user.Name)
	}
	return true
}

func (s *Server) handleHandbook(w http.ResponseWriter, r *http.Request) {
	if !s.handbookGate(w, r) {
		return
	}
	doc, err := s.handbookCurrent(r.Context())
	if err != nil {
		agentStoreError(w, err)
		return
	}
	de := handbookLang(r)
	var volumes []map[string]string
	for _, v := range doc.Facts.Volumes {
		volumes = append(volumes, map[string]string{"name": v.Name, "line": protect.VolumeLine(v, de)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"made": doc.Made, "facts": doc.Facts, "volumes": volumes, "sections": protect.Sections(doc.Facts, de),
	})
}

func (s *Server) handleHandbookDownload(w http.ResponseWriter, r *http.Request) {
	if !s.handbookGate(w, r) {
		return
	}
	doc, err := s.handbookCurrent(r.Context())
	if err != nil {
		agentStoreError(w, err)
		return
	}
	de := handbookLang(r)
	md := protect.Markdown(doc.Facts, doc.Made.In(s.agentLocation()), de)
	name := "Notfallhandbuch"
	if !de {
		name = "Emergency-handbook"
	}
	name += "-" + safeFileName(doc.Facts.Host)
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.md"`)
		_, _ = w.Write([]byte(md))
		return
	}
	pdf, err := office.PDF(md, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pdf", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.pdf"`)
	_, _ = w.Write(pdf)
}

func safeFileName(s string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	if out == "" {
		return "NAS"
	}
	return out
}

func (s *Server) handleHandbookRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.agentSettings().Features.Handbook.Enabled {
		writeError(w, http.StatusNotFound, "feature_off", "the emergency handbook is not switched on")
		return
	}
	doc, err := s.handbookRefresh(r.Context(), true)
	if err != nil {
		if errors.Is(err, agent.ErrNoVolume) {
			agentStoreError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "handbook", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"made": doc.Made})
}
