package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
)

// Skill-Katalog der Gemeinschaft (PLAN 54-G, decision of the owner
// 6.11.2026): the skills come from the App-Store sources, a folder skills/
// in ra5on/TitanOS-Apps and in every own source (package catalog, same
// download, kept in RAM, fetched again after a start and once a day). They
// are trusted like the apps of those sources; there is no key of their own.
//
// Installing takes the entry only when it is exactly what the person was
// shown (digest). It goes through skill_save with its rules (no secrets,
// size) and on top of that every tool the skill names must be one this
// account has. Installed skills are foreign content (untrusted).

const (
	catalogMarker    = "Skill-Katalog: "
	catalogSubmitFmt = "titanos-skill-submission"
)

var (
	catalogIDForm = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// toolWord finds names like files_search in the steps.
	toolWord = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

	errCatalogOff = errors.New("the Skill-Katalog is off")
)

// catalogSkills are the skills of the App-Store sources.
func (s *Server) catalogSkills() []catalog.SkillOf {
	if s.opts.Catalog == nil {
		return nil
	}
	return s.opts.Catalog.Skills()
}

// catalogKey names a skill of a source: "<source>/<id>".
func catalogKey(sk catalog.SkillOf) string { return sk.Source + "/" + sk.ID }

// catalogDigest names exactly the content of an entry.
func catalogDigest(sk catalog.SkillOf) string {
	data, _ := json.Marshal(sk)
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
func unknownTools(box *agentToolbox, e catalog.SkillOf) []string {
	unknown := []string{}
	for _, name := range skillTools(e.Tools, e.Steps) {
		if _, ok := box.tools[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

type catalogView struct {
	catalog.SkillOf
	Key       string   `json:"key"`
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
	own, _ := s.agentStore().Skills(user.Name)
	box := s.newAgentToolbox(user, s.agentSettings())
	out := []catalogView{}
	for _, e := range s.catalogSkills() {
		v := catalogView{SkillOf: e, Key: catalogKey(e), Digest: catalogDigest(e), Unknown: unknownTools(box, e)}
		v.Secret = secretish.MatchString(e.Name + "\n" + e.When + "\n" + e.Steps)
		v.Installed = slices.ContainsFunc(own, func(sk agent.Skill) bool { return sk.Description == catalogMarker+v.Key })
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// handleCatalogInstall installs one entry after the person saw it.
func (s *Server) handleCatalogInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key    string `json:"key"`
		Digest string `json:"digest"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !s.agentSettings().Features.Catalog.Enabled {
		writeError(w, http.StatusConflict, "catalog_off", errCatalogOff.Error())
		return
	}
	skills := s.catalogSkills()
	at := slices.IndexFunc(skills, func(e catalog.SkillOf) bool { return catalogKey(e) == req.Key })
	if at < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such skill in the catalog")
		return
	}
	e := skills[at]
	key := catalogKey(e)
	if catalogDigest(e) != req.Digest {
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
		return strings.EqualFold(sk.Name, strings.TrimSpace(e.Name)) && sk.Description != catalogMarker+key
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
			own[i].Untrusted, own[i].Description = true, catalogMarker+key
		}
	}
	if err := store.SaveSkills(user.Name, own); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": key})
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
	type entry struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description,omitempty"`
		When        string   `json:"when"`
		Steps       string   `json:"steps"`
		Tools       []string `json:"tools"`
	}
	pkg := map[string]any{
		"format": catalogSubmitFmt, "version": 1, "titanos": s.opts.Version, "created": time.Now().UTC().Truncate(time.Second),
		"skill": entry{ID: id, Name: sk.Name, Description: sk.Description, When: sk.When, Steps: sk.Steps, Tools: tools},
		// The folder and file it goes to in the App-Store repository.
		"path": "skills/" + id + "/skill.yaml",
	}
	if strings.HasPrefix(sk.Description, catalogMarker) {
		pkg["skill"] = entry{ID: id, Name: sk.Name, When: sk.When, Steps: sk.Steps, Tools: tools}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="skill-`+id+`.json"`)
	writeJSON(w, http.StatusOK, pkg)
}
