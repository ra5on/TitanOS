package nvr

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Errors of the faces library.
var (
	// ErrFaceNotFound: no such person or picture.
	ErrFaceNotFound = errors.New("nvr: no such face")
	// ErrFaceExists: a person of this name exists.
	ErrFaceExists = errors.New("nvr: a face of this name exists")
	// ErrFaceRejected: Frigate could not use the picture (no face, or
	// more than one).
	ErrFaceRejected = errors.New("nvr: the picture cannot be used")
	// ErrFacesOff: no camera recognises faces, so Frigate's face
	// recognition does not run.
	ErrFacesOff = errors.New("nvr: face recognition is switched off")
)

// Face is one person with the pictures the recognition learned from.
type Face struct {
	Name   string   `json:"name"`
	Images []string `json:"images"`
}

// FaceAttempt is a face Frigate saw and kept for assigning.
type FaceAttempt struct {
	File string `json:"file"`
	// At is when it was seen (Unix seconds; 0 if the name does not say).
	At float64 `json:"at"`
	// Guess is who Frigate took it for ("" for nobody), Score how sure.
	Guess string  `json:"guess"`
	Score float64 `json:"score"`
}

// Faces is the library.
type Faces struct {
	// Enabled is false while no camera recognises faces.
	Enabled  bool          `json:"enabled"`
	Faces    []Face        `json:"faces"`
	Attempts []FaceAttempt `json:"attempts"`
}

var (
	// faceNameRE: letters and digits of any script, "-" and "_". No
	// spaces (Frigate turns them into "_" for the folder) and never
	// "train", Frigate's folder of unassigned faces.
	faceNameRE = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,39}$`)
	// faceFileRE: the names of pictures in the library.
	faceFileRE = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._-]{0,199}\.(webp|png|jpg|jpeg)$`)
)

// ValidFaceName tells whether name can be a person.
func ValidFaceName(name string) bool {
	return faceNameRE.MatchString(name) && name != "train"
}

// ValidFaceFile tells whether file can be a picture of the library.
func ValidFaceFile(file string) bool {
	return faceFileRE.MatchString(file) && !strings.Contains(file, "..")
}

// facesEnabled tells whether an enabled camera recognises faces.
func (m *Manager) facesEnabled() bool {
	for _, c := range m.Cameras() {
		if c.Enabled && c.Detect.Enabled && c.Detect.Faces {
			return true
		}
	}
	return false
}

// facesBase is base, refusing while the recognition is off.
func (m *Manager) facesBase() (string, error) {
	if !m.facesEnabled() {
		return "", ErrFacesOff
	}
	return m.base()
}

// Faces lists the persons and the unassigned faces.
func (m *Manager) Faces(ctx context.Context) (Faces, error) {
	out := Faces{Enabled: m.facesEnabled(), Faces: []Face{}, Attempts: []FaceAttempt{}}
	if !out.Enabled {
		return out, nil
	}
	base, err := m.base()
	if err != nil {
		return out, err
	}
	var raw map[string][]string
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/faces", nil, nil, &raw); err != nil {
		return out, err
	}
	for name, files := range raw {
		files = slices.DeleteFunc(files, func(file string) bool { return !ValidFaceFile(file) })
		slices.Sort(files)
		if name == "train" {
			for _, file := range files {
				out.Attempts = append(out.Attempts, parseAttempt(file))
			}
			continue
		}
		if ValidFaceName(name) {
			out.Faces = append(out.Faces, Face{Name: name, Images: files})
		}
	}
	slices.SortFunc(out.Faces, func(a, b Face) int { return strings.Compare(a.Name, b.Name) })
	// Newest first.
	slices.SortFunc(out.Attempts, func(a, b FaceAttempt) int {
		switch {
		case a.At > b.At:
			return -1
		case a.At < b.At:
			return 1
		}
		return strings.Compare(a.File, b.File)
	})
	return out, nil
}

// parseAttempt reads what Frigate writes into the name of an unassigned
// face: "<event>-<time>-<guess>-<score>.webp".
func parseAttempt(file string) FaceAttempt {
	attempt := FaceAttempt{File: file}
	stem := file[:strings.LastIndex(file, ".")]
	parts := strings.Split(stem, "-")
	if len(parts) < 4 {
		return attempt
	}
	n := len(parts)
	score, err1 := strconv.ParseFloat(parts[n-1], 64)
	at, err2 := strconv.ParseFloat(parts[n-3], 64)
	if err1 != nil || err2 != nil || score < 0 || score > 1 {
		return attempt
	}
	attempt.Score, attempt.At = score, at
	if guess := parts[n-2]; guess != "unknown" && ValidFaceName(guess) {
		attempt.Guess = guess
	}
	return attempt
}

// hasFace tells whether the library has a person (and, with file, that
// picture of it; person "train" are the unassigned faces).
func (m *Manager) hasFace(ctx context.Context, base, person, file string) (bool, []string, error) {
	var raw map[string][]string
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/faces", nil, nil, &raw); err != nil {
		return false, nil, err
	}
	files, ok := raw[person]
	if !ok || (file != "" && !slices.Contains(files, file)) {
		return false, nil, nil
	}
	return true, files, nil
}

// facesError maps Frigate's refusals. Any other answer of Frigate loses
// its message: Frigate's texts about faces name the person ("Invalid
// name: …", "Successfully renamed face to …"), and the error may be
// logged.
func facesError(err error) error {
	switch status := frigateStatus(err); status {
	case 0:
		return err
	case http.StatusBadRequest:
		return ErrFaceRejected
	case http.StatusNotFound:
		return ErrFaceNotFound
	default:
		return &frigateError{Status: status, Message: "(about faces; not logged)"}
	}
}

// CreateFace adds a person without pictures.
func (m *Manager) CreateFace(ctx context.Context, name string) error {
	if !ValidFaceName(name) {
		return invalid("name")
	}
	base, err := m.facesBase()
	if err != nil {
		return err
	}
	if exists, _, err := m.hasFace(ctx, base, name, ""); err != nil {
		return err
	} else if exists {
		return ErrFaceExists
	}
	return facesError(m.frigate.call(ctx, http.MethodPost, base, "/api/faces/"+url.PathEscape(name)+"/create", nil, nil, nil))
}

// RenameFace gives a person another name.
func (m *Manager) RenameFace(ctx context.Context, name, to string) error {
	if !ValidFaceName(name) {
		return ErrFaceNotFound
	}
	if !ValidFaceName(to) {
		return invalid("name")
	}
	base, err := m.facesBase()
	if err != nil {
		return err
	}
	if exists, _, err := m.hasFace(ctx, base, name, ""); err != nil {
		return err
	} else if !exists {
		return ErrFaceNotFound
	}
	if exists, _, err := m.hasFace(ctx, base, to, ""); err != nil {
		return err
	} else if exists {
		return ErrFaceExists
	}
	return facesError(m.frigate.call(ctx, http.MethodPut, base, "/api/faces/"+url.PathEscape(name)+"/rename", nil, map[string]any{"new_name": to}, nil))
}

// DeleteFace removes a person with its pictures, or one picture (file).
// Person "train" with a file discards an unassigned face.
func (m *Manager) DeleteFace(ctx context.Context, name, file string) error {
	if (!ValidFaceName(name) && !(name == "train" && file != "")) || (file != "" && !ValidFaceFile(file)) {
		return ErrFaceNotFound
	}
	base, err := m.facesBase()
	if err != nil {
		return err
	}
	exists, files, err := m.hasFace(ctx, base, name, file)
	if err != nil {
		return err
	}
	if !exists {
		return ErrFaceNotFound
	}
	// Frigate removes the person when every picture of it is named.
	ids := files
	if file != "" {
		ids = []string{file}
	}
	if ids == nil {
		ids = []string{}
	}
	return facesError(m.frigate.call(ctx, http.MethodPost, base, "/api/faces/"+url.PathEscape(name)+"/delete", nil, map[string]any{"ids": ids}, nil))
}

// MaxFaceImage bounds an uploaded picture.
const MaxFaceImage = 8 << 20

// AddFaceImage teaches a person one more picture. Frigate takes it only
// if it shows a face it can use. The picture is never logged.
func (m *Manager) AddFaceImage(ctx context.Context, name, contentType string, image []byte) error {
	if !ValidFaceName(name) {
		return ErrFaceNotFound
	}
	extension := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[contentType]
	if extension == "" || len(image) == 0 || len(image) > MaxFaceImage {
		return invalid("picture: JPEG, PNG or WebP of at most 8 MB")
	}
	base, err := m.facesBase()
	if err != nil {
		return err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="face.`+extension+`"`)
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(image); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/faces/"+url.PathEscape(name)+"/register", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := m.frigate.stream.Do(req)
	if err != nil {
		return withoutURL(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20)) // the answer only says yes or no
	switch {
	case res.StatusCode == http.StatusBadRequest:
		return ErrFaceRejected
	case res.StatusCode >= 400:
		return &frigateError{Status: res.StatusCode, Message: "register face"}
	}
	return nil
}

// AssignAttempt gives an unassigned face to a person (it is made if it
// does not exist): Frigate learns the face from it.
func (m *Manager) AssignAttempt(ctx context.Context, file, name string) error {
	if !ValidFaceFile(file) {
		return ErrFaceNotFound
	}
	if !ValidFaceName(name) {
		return invalid("name")
	}
	base, err := m.facesBase()
	if err != nil {
		return err
	}
	if exists, _, err := m.hasFace(ctx, base, "train", file); err != nil {
		return err
	} else if !exists {
		return ErrFaceNotFound
	}
	return facesError(m.frigate.call(ctx, http.MethodPost, base, "/api/faces/train/"+url.PathEscape(name)+"/classify", nil, map[string]any{"training_file": file}, nil))
}

// FaceImagePath is Frigate's path of a picture of the library (person
// "train": an unassigned face), after checking that it is listed.
func (m *Manager) FaceImagePath(ctx context.Context, person, file string) (string, error) {
	if (!ValidFaceName(person) && person != "train") || !ValidFaceFile(file) {
		return "", ErrFaceNotFound
	}
	base, err := m.facesBase()
	if err != nil {
		return "", err
	}
	if exists, _, err := m.hasFace(ctx, base, person, file); err != nil {
		return "", err
	} else if !exists {
		return "", ErrFaceNotFound
	}
	return "/clips/faces/" + url.PathEscape(person) + "/" + url.PathEscape(file), nil
}

// RecognizedPlate is a license plate the NVR has read.
type RecognizedPlate struct {
	Plate string `json:"plate"`
	// Count and LastSeen come from the newest events (0 and null for a
	// plate that only older events hold); Camera is where it was last seen.
	Count    int      `json:"count"`
	LastSeen *float64 `json:"lastSeen"`
	Camera   string   `json:"camera"`
}

// RecognizedPlates lists the plates Frigate has read, most often seen
// first.
func (m *Manager) RecognizedPlates(ctx context.Context) ([]RecognizedPlate, error) {
	base, err := m.base()
	if err != nil {
		return nil, err
	}
	var all []string
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/recognized_license_plates", nil, nil, &all); err != nil {
		return nil, err
	}
	byPlate := map[string]*RecognizedPlate{}
	for _, plate := range all {
		if plate = clip(strings.TrimSpace(plate), 40); plate != "" {
			byPlate[plate] = &RecognizedPlate{Plate: plate}
		}
	}
	// Counts from the newest vehicles (one request, not one per plate).
	var events []frigateEvent
	query := url.Values{"labels": {"car,motorcycle,bus"}, "limit": {"1000"}, "include_thumbnails": {"0"}}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/events", query, nil, &events); err == nil {
		for _, raw := range events {
			e := raw.event()
			entry := byPlate[e.Plate]
			if e.Plate == "" || entry == nil {
				continue
			}
			entry.Count++
			if entry.LastSeen == nil || e.Start > *entry.LastSeen {
				start := e.Start
				entry.LastSeen, entry.Camera = &start, e.Camera
			}
		}
	}
	out := make([]RecognizedPlate, 0, len(byPlate))
	for _, entry := range byPlate {
		out = append(out, *entry)
	}
	slices.SortFunc(out, func(a, b RecognizedPlate) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Plate, b.Plate)
	})
	return out, nil
}
