package api

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Skill-Katalog der Gemeinschaft (PLAN 54-G). A catalog is one JSON index
// at the configured https address and its detached ed25519 signature at
// the same address plus ".sig" (base64 of the 64 signature bytes over the
// exact bytes of the index). Nothing is parsed before the signature
// checks out; without a key or a valid signature nothing is shown and
// nothing installed.
//
// Installing re-reads and re-checks the catalog on the server and takes the
// entry only when it is byte for byte what the person was shown (digest).
// It goes through skill_save with its rules (no secrets, size) and on top
// of that every tool the skill names must be one this account has.

const (
	maxCatalogIndex  = 1 << 20
	maxCatalogSig    = 1 << 10
	catalogMarker    = "Skill-Katalog: "
	catalogSubmitFmt = "titanos-skill-submission"
)

// catalogIndex is the signed file.
type catalogIndex struct {
	Version int            `json:"version"`
	Skills  []catalogEntry `json:"skills"`
}

type catalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	When        string   `json:"when"`
	Steps       string   `json:"steps"`
	Tools       []string `json:"tools"`
	Author      string   `json:"author,omitempty"`
	Version     string   `json:"version,omitempty"`
	Installs    int      `json:"installs,omitempty"`
}

var (
	catalogIDForm = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// toolWord finds names like files_search in the steps.
	toolWord = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

	errCatalogOff       = errors.New("the Skill-Katalog is off or has no address and key")
	errCatalogSignature = errors.New("the signature of the catalog is missing or not valid; nothing is installed")
)

// catalogHTTP fetches the catalog (tests replace it).
var catalogHTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errors.New("too many redirects")
	}
	return nil
}}

func catalogGet(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := catalogHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the catalog is not reachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the catalog answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the catalog is too large")
	}
	return data, nil
}

// verifyCatalog checks the signature over the raw bytes, then parses.
func verifyCatalog(index, sig []byte, key string) (catalogIndex, error) {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return catalogIndex{}, errCatalogOff
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(pub), index, raw) {
		return catalogIndex{}, errCatalogSignature
	}
	var out catalogIndex
	if err := json.Unmarshal(index, &out); err != nil || out.Version != 1 {
		return catalogIndex{}, errors.New("the catalog has an unknown format")
	}
	seen := map[string]bool{}
	out.Skills = slices.DeleteFunc(out.Skills, func(e catalogEntry) bool {
		bad := !catalogIDForm.MatchString(e.ID) || seen[e.ID]
		seen[e.ID] = true
		return bad
	})
	return out, nil
}

func (s *Server) fetchCatalog(ctx context.Context) (catalogIndex, error) {
	c := s.agentSettings().Features.Catalog
	if !c.Enabled || c.URL == "" || c.Key == "" {
		return catalogIndex{}, errCatalogOff
	}
	index, err := catalogGet(ctx, c.URL, maxCatalogIndex)
	if err != nil {
		return catalogIndex{}, err
	}
	sig, err := catalogGet(ctx, c.URL+".sig", maxCatalogSig)
	if err != nil {
		return catalogIndex{}, errCatalogSignature
	}
	return verifyCatalog(index, sig, c.Key)
}

// digest names exactly the content of an entry.
func (e catalogEntry) digest() string {
	data, _ := json.Marshal(e)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// skillTools are the tool names a skill names: declared and in its steps.
func skillTools(declared []string, steps string) []string {
	out := slices.Clone(declared)
	for _, w := range toolWord.FindAllString(steps, -1) {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// unknownTools are the tools of an entry this account does not have.
func unknownTools(box *agentToolbox, e catalogEntry) []string {
	unknown := []string{}
	for _, name := range skillTools(e.Tools, e.Steps) {
		if _, ok := box.tools[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

type catalogView struct {
	catalogEntry
	Digest    string   `json:"digest"`
	Unknown   []string `json:"unknown"`
	Installed bool     `json:"installed"`
	Secret    bool     `json:"secret,omitempty"`
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !s.agentSettings().Features.Catalog.Enabled {
		writeError(w, http.StatusConflict, "catalog_off", errCatalogOff.Error())
		return
	}
	index, err := s.fetchCatalog(r.Context())
	if err != nil {
		code := "catalog_unavailable"
		if errors.Is(err, errCatalogSignature) {
			code = "catalog_signature"
		} else if errors.Is(err, errCatalogOff) {
			code = "catalog_off"
		}
		writeError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	own, _ := s.agentStore().Skills(user.Name)
	box := s.newAgentToolbox(user, s.agentSettings())
	out := []catalogView{}
	for _, e := range index.Skills {
		v := catalogView{catalogEntry: e, Digest: e.digest(), Unknown: unknownTools(box, e)}
		if v.Tools == nil {
			v.Tools = []string{}
		}
		v.Secret = secretish.MatchString(e.Name + "\n" + e.When + "\n" + e.Steps)
		v.Installed = slices.ContainsFunc(own, func(sk agent.Skill) bool { return sk.Description == catalogMarker+e.ID })
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// handleCatalogInstall installs one entry after the person saw it.
func (s *Server) handleCatalogInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Digest string `json:"digest"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	index, err := s.fetchCatalog(r.Context())
	if err != nil {
		code := "catalog_unavailable"
		if errors.Is(err, errCatalogSignature) {
			code = "catalog_signature"
		} else if errors.Is(err, errCatalogOff) {
			code = "catalog_off"
		}
		writeError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	at := slices.IndexFunc(index.Skills, func(e catalogEntry) bool { return e.ID == req.ID })
	if at < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such skill in the catalog")
		return
	}
	e := index.Skills[at]
	if e.digest() != req.Digest {
		writeError(w, http.StatusConflict, "catalog_changed", "the skill changed in the catalog since it was shown; look at it again")
		return
	}
	box := s.newAgentToolbox(user, s.agentSettings())
	if unknown := unknownTools(box, e); len(unknown) > 0 {
		writeError(w, http.StatusBadRequest, "catalog_unknown_tool", "the skill uses tools this account does not have: "+strings.Join(unknown, ", "))
		return
	}
	store := s.agentStore()
	own, err := store.Skills(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if slices.ContainsFunc(own, func(sk agent.Skill) bool {
		return strings.EqualFold(sk.Name, strings.TrimSpace(e.Name)) && sk.Description != catalogMarker+e.ID
	}) {
		writeError(w, http.StatusConflict, "skill_exists", "there is already an own skill with this name")
		return
	}
	args, _ := json.Marshal(map[string]string{"name": e.Name, "when": e.When, "steps": e.Steps})
	if _, err := box.skillSave(r.Context(), nil, args); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_skill", err.Error())
		return
	}
	// From the community: foreign content, marked as such.
	own, err = store.Skills(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	for i := range own {
		if strings.EqualFold(own[i].Name, strings.TrimSpace(e.Name)) {
			own[i].Untrusted, own[i].Description = true, catalogMarker+e.ID
		}
	}
	if err := store.SaveSkills(user.Name, own); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": e.ID})
}

// handleCatalogExport makes a submission package of an own skill, to send
// to whoever runs the catalog (submitting itself is not part of TitanOS).
func (s *Server) handleCatalogExport(w http.ResponseWriter, r *http.Request) {
	if !s.agentSettings().Features.Catalog.Enabled {
		writeError(w, http.StatusConflict, "catalog_off", errCatalogOff.Error())
		return
	}
	user := currentUser(r)
	own, err := s.agentStore().Skills(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	at := slices.IndexFunc(own, func(sk agent.Skill) bool { return sk.ID == r.PathValue("id") })
	if at < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such skill")
		return
	}
	sk := own[at]
	if secretish.MatchString(sk.Name + "\n" + sk.When + "\n" + sk.Steps) {
		writeError(w, http.StatusBadRequest, "invalid_skill", "skills with passwords, keys or secrets are not shared")
		return
	}
	box := s.newAgentToolbox(user, s.agentSettings())
	tools := []string{}
	for _, name := range skillTools(nil, sk.Steps) {
		if _, ok := box.tools[name]; ok {
			tools = append(tools, name)
		}
	}
	id := strings.Trim(nonID.ReplaceAllString(strings.ToLower(sk.Name), "-"), "-")
	if id == "" || !catalogIDForm.MatchString(id) {
		id = "skill"
	}
	pkg := map[string]any{
		"format": catalogSubmitFmt, "version": 1, "titanos": s.opts.Version, "created": time.Now().UTC().Truncate(time.Second),
		"skill": catalogEntry{ID: id, Name: sk.Name, Description: sk.Description, When: sk.When, Steps: sk.Steps, Tools: tools},
	}
	if strings.HasPrefix(sk.Description, catalogMarker) {
		pkg["skill"] = catalogEntry{ID: id, Name: sk.Name, When: sk.When, Steps: sk.Steps, Tools: tools}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="skill-`+id+`.json"`)
	writeJSON(w, http.StatusOK, pkg)
}
