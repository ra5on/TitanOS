package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"path"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// The folders and mails of a mailbox, attachments and sending (T-Mail,
// docs/TMAIL.md "API").

func (a tmailArea) handleFolders(w http.ResponseWriter, r *http.Request) {
	list, err := a.mail.Folders(r.Context(), currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		writeMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a tmailArea) handleFolderCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.mail.CreateFolder(r.Context(), currentUser(r).Name, r.PathValue("id"), req.Path); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a tmailArea) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		To   string `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.mail.RenameFolder(r.Context(), currentUser(r).Name, r.PathValue("id"), req.Path, req.To); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a tmailArea) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.mail.DeleteFolder(r.Context(), currentUser(r).Name, r.PathValue("id"), r.URL.Query().Get("path")); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queryUID reads a UID parameter; 0 if it is missing or not a number.
func queryUID(r *http.Request, name string) uint32 {
	n, err := strconv.ParseUint(r.URL.Query().Get(name), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func queryBool(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}

func (a tmailArea) handleMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := a.mail.List(r.Context(), currentUser(r).Name, r.PathValue("id"), tmail.ListQuery{Folder: q.Get("folder"),
		Before: queryUID(r, "before"), Limit: limit, Q: q.Get("q"), Unread: queryBool(r, "unread"), Flagged: queryBool(r, "flagged")})
	if err != nil {
		writeMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a tmailArea) handleMessage(w http.ResponseWriter, r *http.Request) {
	uid := queryUID(r, "uid")
	if uid == 0 {
		writeError(w, http.StatusBadRequest, "mail_invalid", "uid is required")
		return
	}
	msg, err := a.mail.Read(r.Context(), currentUser(r).Name, r.PathValue("id"), r.URL.Query().Get("folder"), uid, queryBool(r, "images"))
	if err != nil {
		writeMailError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, msg)
}

// partWriter sets the headers of a download at its first byte, so that an
// error before it is still answered as JSON.
type partWriter struct {
	w       http.ResponseWriter
	headers func(h http.Header)
	started bool
}

func (p *partWriter) start() {
	if !p.started {
		p.started = true
		p.headers(p.w.Header())
		p.w.WriteHeader(http.StatusOK)
	}
}

func (p *partWriter) Write(b []byte) (int, error) {
	p.start()
	return p.w.Write(b)
}

// handlePart hands out an attachment: always as a download, never shown
// by the browser under the address of TitanOS (not HTML, SVG or PDF
// either).
func (a tmailArea) handlePart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pw := &partWriter{w: w}
	err := a.mail.Part(r.Context(), currentUser(r).Name, r.PathValue("id"), q.Get("folder"), queryUID(r, "uid"), q.Get("part"),
		func(info tmail.PartInfo) io.Writer {
			pw.headers = func(h http.Header) {
				h.Set("Content-Type", "application/octet-stream")
				h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name}))
				h.Set("X-Content-Type-Options", "nosniff")
				h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
				h.Set("Cache-Control", "no-store")
			}
			return pw
		})
	a.finishPart(w, pw, err)
}

func (a tmailArea) finishPart(w http.ResponseWriter, pw *partWriter, err error) {
	switch {
	case err == nil:
		pw.start() // an empty part
	case !pw.started:
		writeMailError(w, err)
	}
}

// handleCID hands out a picture embedded in a mail (cid:): only PNG, JPEG,
// GIF and WebP, inline, without scripts.
func (a tmailArea) handleCID(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pw := &partWriter{w: w}
	err := a.mail.CID(r.Context(), currentUser(r).Name, r.PathValue("id"), q.Get("folder"), queryUID(r, "uid"), q.Get("cid"),
		func(info tmail.PartInfo) io.Writer {
			pw.headers = func(h http.Header) {
				h.Set("Content-Type", info.Type)
				h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": info.Name}))
				h.Set("X-Content-Type-Options", "nosniff")
				h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
				h.Set("Cache-Control", "private, no-store")
			}
			return pw
		})
	a.finishPart(w, pw, err)
}

// maxSaveBytes bounds an attachment saved to the NAS: it passes through
// RAM.
const maxSaveBytes = 64 << 20

// rootOf finds a root of the files of the account behind a request; the
// same answer for "missing" and "not yours".
func (a tmailArea) rootOf(w http.ResponseWriter, r *http.Request, id string) (files.Root, bool) {
	for _, root := range a.roots(r) {
		if root.ID == id {
			return root, true
		}
	}
	writeError(w, http.StatusNotFound, "file_not_found", "no such share")
	return files.Root{}, false
}

// handlePartSave saves an attachment into the files of the NAS with the
// rights of the account: {folder, uid, part, root, path}. The file must
// not exist yet.
func (a tmailArea) handlePartSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
		UID    uint32 `json:"uid"`
		Part   string `json:"part"`
		Root   string `json:"root"`
		Path   string `json:"path"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := a.rootOf(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	data, _, err := a.mail.PartBytes(r.Context(), user.Name, r.PathValue("id"), req.Folder, req.UID, req.Part, maxSaveBytes)
	if err != nil {
		writeMailError(w, err)
		return
	}
	noteTarget(r, filePlace(req.Root, req.Path))
	result, err := a.files.WriteContent(root, req.Path, bytes.NewReader(data), int64(len(data)), user.UID, files.Expectation{None: true})
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type mailUIDs struct {
	Folder string   `json:"folder"`
	UIDs   []uint32 `json:"uids"`
}

func (a tmailArea) handleFlags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		mailUIDs
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.mail.SetFlags(r.Context(), currentUser(r).Name, r.PathValue("id"), req.Folder, req.UIDs, req.Add, req.Remove); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a tmailArea) handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		mailUIDs
		To string `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.mail.Move(r.Context(), currentUser(r).Name, r.PathValue("id"), req.Folder, req.UIDs, req.To); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a tmailArea) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req mailUIDs
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.mail.Delete(r.Context(), currentUser(r).Name, r.PathValue("id"), req.Folder, req.UIDs); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recipients take "Name <addr>", "addr" or {name, addr}.
type recipients []tmail.Address

func (rs *recipients) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, item := range raw {
		var text string
		if json.Unmarshal(item, &text) == nil {
			a, err := parseRecipient(text)
			if err != nil {
				return err
			}
			*rs = append(*rs, a)
			continue
		}
		var a tmail.Address
		if err := json.Unmarshal(item, &a); err != nil {
			return err
		}
		*rs = append(*rs, a)
	}
	return nil
}

func parseRecipient(text string) (tmail.Address, error) {
	a, err := mail.ParseAddress(text)
	if err != nil {
		return tmail.Address{}, err
	}
	return tmail.Address{Name: a.Name, Addr: a.Address}, nil
}

func extOf(name string) string { return path.Ext(name) }

// sendMeta is the part "meta" of POST …/send and …/draft.
type sendMeta struct {
	To         recipients `json:"to"`
	Cc         recipients `json:"cc"`
	Bcc        recipients `json:"bcc"`
	Subject    string     `json:"subject"`
	Text       string     `json:"text"`
	InReplyTo  string     `json:"inReplyTo"`
	References string     `json:"references"`
	Signature  string     `json:"signature"`
	DraftUID   uint32     `json:"draftUid"`
	// AttachFrom takes parts of a mail along (forwarding, a draft opened
	// again).
	AttachFrom *struct {
		Folder string   `json:"folder"`
		UID    uint32   `json:"uid"`
		Parts  []string `json:"parts"`
	} `json:"attachFrom"`
}

// nasFile names a file of the NAS by root and path (GET /files/roots).
type nasFile struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

// handleSend sends a mail (draft=false) or keeps it as a draft: multipart
// with "meta" (JSON), files as "file" and "nasFiles" (JSON list of {root,
// path}, read with the account's rights).
func (a tmailArea) handleSend(draft bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		id := r.PathValue("id")
		r.Body = http.MaxBytesReader(w, r.Body, tmail.MaxAttachments+(4<<20))
		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, "mail_invalid", "a multipart body with meta is required")
			return
		}
		var meta *sendMeta
		var attached []tmail.File
		var nas []nasFile
		left := int64(tmail.MaxAttachments)
		tooLarge := func() { writeMailError(w, tmail.ErrTooLarge) }
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					tooLarge()
					return
				}
				writeError(w, http.StatusBadRequest, "mail_invalid", "the multipart body could not be read")
				return
			}
			switch part.FormName() {
			case "meta":
				meta = &sendMeta{}
				if err := json.NewDecoder(io.LimitReader(part, 8<<20)).Decode(meta); err != nil {
					writeError(w, http.StatusBadRequest, "mail_invalid", "meta: "+err.Error())
					return
				}
			case "nasFiles":
				if err := json.NewDecoder(io.LimitReader(part, 64<<10)).Decode(&nas); err != nil || len(nas) > 100 {
					writeError(w, http.StatusBadRequest, "mail_invalid", "nasFiles")
					return
				}
			case "file":
				data, err := io.ReadAll(io.LimitReader(part, left+1))
				if err != nil {
					var maxErr *http.MaxBytesError
					if errors.As(err, &maxErr) {
						tooLarge()
						return
					}
					writeError(w, http.StatusBadRequest, "mail_invalid", "file")
					return
				}
				if int64(len(data)) > left {
					tooLarge()
					return
				}
				left -= int64(len(data))
				attached = append(attached, tmail.File{Name: part.FileName(), Type: part.Header.Get("Content-Type"), Data: data})
			}
		}
		if meta == nil {
			writeError(w, http.StatusBadRequest, "mail_invalid", "meta is required")
			return
		}
		for _, f := range nas {
			root, ok := a.rootOf(w, r, f.Root)
			if !ok {
				return
			}
			file, info, err := a.files.Open(root, f.Path)
			if err != nil {
				writeFileError(w, err)
				return
			}
			if !info.Mode().IsRegular() || info.Size() > left {
				file.Close()
				if info.Mode().IsRegular() {
					tooLarge()
				} else {
					writeError(w, http.StatusBadRequest, "mail_invalid", "only files can be attached")
				}
				return
			}
			data, err := io.ReadAll(io.LimitReader(file, left+1))
			file.Close()
			if err != nil || int64(len(data)) > left {
				tooLarge()
				return
			}
			left -= int64(len(data))
			attached = append(attached, tmail.File{Name: info.Name(), Type: mime.TypeByExtension(extOf(info.Name())), Data: data})
		}
		if from := meta.AttachFrom; from != nil {
			if len(from.Parts) > 100 {
				writeError(w, http.StatusBadRequest, "mail_invalid", "too many parts")
				return
			}
			for _, part := range from.Parts {
				data, info, err := a.mail.PartBytes(r.Context(), user.Name, id, from.Folder, from.UID, part, left)
				if err != nil {
					writeMailError(w, err)
					return
				}
				left -= int64(len(data))
				attached = append(attached, tmail.File{Name: info.Name, Type: info.Type, Data: data})
			}
		}
		out := tmail.Outgoing{To: meta.To, Cc: meta.Cc, Bcc: meta.Bcc, Subject: meta.Subject, Text: meta.Text, InReplyTo: meta.InReplyTo,
			References: meta.References, Signature: meta.Signature, DraftUID: meta.DraftUID, Files: attached}
		if draft {
			uid, err := a.mail.SaveDraft(r.Context(), user.Name, id, out)
			if err != nil {
				writeMailError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]uint32{"uid": uid})
			return
		}
		res, err := a.mail.Send(r.Context(), user.Name, id, out)
		if err != nil {
			writeMailError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
