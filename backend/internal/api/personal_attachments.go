package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const personalAttachmentMax = 10 << 20

type personalAttachment struct {
	Hash string `json:"sha256,omitempty"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int    `json:"size"`
}

func (f personalAttachment) digest() string {
	if f.Hash != "" {
		return f.Hash
	}
	return f.ID
}
func (f personalAttachment) source() pa.Source {
	return pa.Source{Kind: "attachment", ID: f.ID, Label: f.Name}
}
func attachmentBlob(u auth.User, id string) string { return personalKey(u) + "-" + id + ".pdf" }
func (s *Server) registerPersonalAttachments(mux *routeMux) {
	p := s.personalBrowser
	mux.HandleFunc("POST /api/v1/personal-agent/attachments", p(s.personalUpload))
	mux.HandleFunc("GET /api/v1/personal-agent/attachments/{id}/content", p(s.personalAttachmentContent))
	mux.HandleFunc("GET /api/v1/personal-agent/attachments", p(s.personalAttachments))
	mux.HandleFunc("POST /api/v1/personal-agent/attachments/{id}/delete", p(s.personalDeleteAttachment))
}
func (s *Server) personalAttachments(w http.ResponseWriter, r *http.Request) {
	out := []pa.Source{}
	err := s.personalTxn(currentUser(r), false, func(a *personalAccount) error {
		for _, f := range a.Attachments {
			out = append(out, f.source())
		}
		return nil
	})
	personalRespond(w, map[string]any{"sources": out}, err)
}
func (s *Server) personalUpload(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	cap, err := s.personalEffective(u, "attachment.read")
	if err != nil || cap.Effective == pa.Never {
		personalRespond(w, nil, errPersonalDenied)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || name != path.Base(name) || len(name) > 240 || strings.Contains(name, "\\") || strings.ContainsFunc(name, unicode.IsControl) || !strings.EqualFold(path.Ext(name), ".pdf") {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	if s.opts.MemoryPolicy != nil {
		release, ok := s.opts.MemoryPolicy.TryReserve(personalAttachmentMax)
		if !ok {
			personalRespond(w, nil, errPersonalFull)
			return
		}
		defer release()
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, personalAttachmentMax))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "PDF limit is 10 MiB")
		return
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	digest := sha256.Sum256(data)
	f := personalAttachment{ID: personalID(), Hash: hex.EncodeToString(digest[:]), Name: name, Size: len(data)}
	s.personal.mu.Lock()
	defer s.personal.mu.Unlock()
	root, release, err := s.personalRoot()
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	defer release()
	defer root.Close()
	created := false
	err = s.personalAccountTx(root, u, true, func(a *personalAccount) error {
		if personalCap(s.personal.tools["attachment.read"], a).Effective == pa.Never {
			return errPersonalDenied
		}
		used := 0
		for _, old := range a.Attachments {
			if old.digest() == f.Hash {
				f = old
				return nil
			}
			used += old.Size
		}
		if len(a.Attachments) >= 128 || used+f.Size > 256<<20 {
			return errPersonalFull
		}
		if err := volumedata.Write(root, attachmentBlob(u, f.ID), data); err != nil {
			return err
		}
		created = true
		a.Attachments = append(a.Attachments, f)
		return nil
	})
	if err != nil && created {
		_ = root.Remove(attachmentBlob(u, f.ID))
	}
	personalRespond(w, f.source(), err)
}
func (s *Server) personalDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := r.PathValue("id")
	var confirm struct {
		Confirm string `json:"confirm"`
	}
	if !readPersonalJSON(w, r, &confirm) {
		return
	}
	if confirm.Confirm != id {
		personalRespond(w, nil, errPersonalConflict)
		return
	}
	s.personal.mu.Lock()
	defer s.personal.mu.Unlock()
	root, release, err := s.personalRoot()
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	defer release()
	defer root.Close()
	err = s.personalAccountTx(root, u, true, func(a *personalAccount) error {
		i := slices.IndexFunc(a.Attachments, func(f personalAttachment) bool { return f.ID == id })
		if i < 0 {
			return errPersonalMissing
		}
		a.Attachments = slices.Delete(a.Attachments, i, i+1)
		return nil
	})
	if err == nil {
		err = root.Remove(attachmentBlob(u, id))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	personalRespond(w, map[string]bool{"deleted": err == nil}, err)
}
func (s *Server) personalAttachment(ctx context.Context, u auth.User, r pa.Request, read bool) (pa.Preview, error) {
	if len(r.Sources) != 1 || r.Sources[0].Kind != "attachment" {
		return pa.Preview{}, errPersonalInvalid
	}
	src := r.Sources[0]
	var record personalAttachment
	err := s.personalTxn(u, false, func(a *personalAccount) error {
		for _, f := range a.Attachments {
			if f.ID == src.ID {
				record = f
				return nil
			}
		}
		return errPersonalMissing
	})
	if err != nil {
		return pa.Preview{}, err
	}
	first, last := 1, 5
	for k, v := range src.Ref {
		n, err := strconv.Atoi(v)
		if err != nil {
			return pa.Preview{}, errPersonalInvalid
		}
		switch k {
		case "first":
			first = n
		case "last":
			last = n
		default:
			return pa.Preview{}, errPersonalInvalid
		}
	}
	if first < 1 || last < first || last-first >= 20 || last > 10000 {
		return pa.Preview{}, errPersonalInvalid
	}
	src.Label = record.Name
	preview := pa.Preview{Title: record.Name, Sources: []pa.Source{src}}
	if !read {
		return preview, nil
	}
	root, release, err := s.personalRoot()
	if err != nil {
		return preview, err
	}
	defer release()
	defer root.Close()
	file, err := root.Open(attachmentBlob(u, record.ID))
	if err != nil {
		return preview, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, personalAttachmentMax+1))
	digest := sha256.Sum256(data)
	if err != nil || len(data) != record.Size || hex.EncodeToString(digest[:]) != record.digest() {
		return preview, errors.New("invalid personal PDF")
	}
	pages, err := office.ReadPDF(ctx, data, first, last)
	if err != nil {
		return preview, err
	}
	var out strings.Builder
	for _, page := range pages.Pages {
		fmt.Fprintf(&out, "[%d] %s\n", page.Number, page.Text)
	}
	preview.Text = personalLimitText(out.String(), 16000)
	return preview, nil
}
func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		return []personalTool{{ID: "attachment.read", Title: "Persönliches PDF lesen", App: "files", SourceKinds: []string{"attachment"}, Default: pa.Ask,
			Check: func(ctx context.Context, u auth.User, r pa.Request) error {
				_, err := s.personalAttachment(ctx, u, r, false)
				return err
			},
			Prepare: func(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
				return s.personalAttachment(ctx, u, r, true)
			},
		}}
	})
}

func (s *Server) personalAttachmentContent(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := r.PathValue("id")
	c, err := s.personalEffective(u, "attachment.read")
	if err != nil || c.Effective == pa.Never {
		personalRespond(w, nil, errPersonalDenied)
		return
	}
	var selected personalAttachment
	err = s.personalTxn(u, false, func(a *personalAccount) error {
		for _, f := range a.Attachments {
			if f.ID == id {
				selected = f
				return nil
			}
		}
		return errPersonalMissing
	})
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	root, release, err := s.personalRoot()
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	defer release()
	defer root.Close()
	f, err := root.Open(attachmentBlob(u, id))
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() != int64(selected.Size) {
		personalRespond(w, nil, errPersonalConflict)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": selected.Name}))
	http.ServeContent(w, r, selected.Name, time.Time{}, f)
}
