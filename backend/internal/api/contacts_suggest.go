package api

import (
	"net/http"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// Suggestions from T-Mail (TECHNIK 7.2): the addresses T-Mail learned from
// sent mail that are in no contact yet. Only with the account's T-Mail
// grant (else an empty list), only here – never a tool of the agent, never
// MCP (the routes are browserOnly like every route of Kontakte).

// maxContactSuggestions bounds the list (T-Mail keeps at most 2000).
const maxContactSuggestions = 500

type contactSuggestion struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	// Count is the number of mails sent to the address.
	Count int `json:"count"`
}

// contactAddresses are the e-mail addresses in the account's contacts
// (lower case, groups left out), from the index.
func contactAddresses(store *pimstore.Store, user string) (map[string]bool, error) {
	cols, err := store.Collections(pimstore.Contacts, user)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, c := range cols {
		objects, err := store.Objects(pimstore.Contacts, user, c.ID)
		if err != nil {
			return nil, err
		}
		for _, o := range objects {
			if o.Group {
				continue
			}
			for _, e := range o.Emails {
				known[strings.ToLower(strings.TrimSpace(e))] = true
			}
		}
	}
	return known, nil
}

// learned is what T-Mail learned for the account minus known addresses;
// nil without the grant.
func (a pimDataArea) learned(user, q string, limit int, known map[string]bool) []tmail.Contact {
	if a.mail == nil || !a.mail.Allowed(user) {
		return nil
	}
	return a.mail.Learned(user, q, limit, func(addr string) bool { return known[strings.ToLower(strings.TrimSpace(addr))] })
}

func (a pimDataArea) handleContactSuggestions(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	user := currentUser(r).Name
	known, err := contactAddresses(store, user)
	if err != nil {
		contactsFail(w, r, "", err, "contact_not_found")
		return
	}
	out := []contactSuggestion{}
	for _, c := range a.learned(user, r.URL.Query().Get("q"), maxContactSuggestions, known) {
		out = append(out, contactSuggestion{Name: c.Name, Addr: c.Addr, Count: c.Count})
	}
	writeJSON(w, http.StatusOK, out)
}

// splitName makes first and last name of a name T-Mail learned: "Last,
// First", else the last word is the last name; one word is a first name.
func splitName(name string) (first, last string) {
	name = strings.Trim(strings.TrimSpace(name), `"'`)
	if l, f, ok := strings.Cut(name, ","); ok {
		return strings.TrimSpace(f), strings.TrimSpace(l)
	}
	words := strings.Fields(name)
	switch len(words) {
	case 0:
		return "", ""
	case 1:
		return words[0], ""
	}
	return strings.Join(words[:len(words)-1], " "), words[len(words)-1]
}

// handleContactFromSuggestion makes a contact of a learned address (name
// and address). The learned entry stays in T-Mail and drops out of the
// suggestions because a contact has the address now.
func (a pimDataArea) handleContactFromSuggestion(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	var q struct {
		Addr string `json:"addr"`
		Book string `json:"book"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if q.Book == "" {
		q.Book = pimstore.DefaultBook
	}
	user := currentUser(r).Name
	addr := strings.ToLower(strings.TrimSpace(q.Addr))
	known, err := contactAddresses(store, user)
	if err != nil {
		contactsFail(w, r, q.Book, err, "contact_not_found")
		return
	}
	var found *tmail.Contact
	if addr != "" {
		for _, c := range a.learned(user, addr, maxContactSuggestions, nil) {
			if strings.ToLower(strings.TrimSpace(c.Addr)) == addr {
				found = &c
				break
			}
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "contact_suggestion_not_found", contactsMessages["contact_suggestion_not_found"])
		return
	}
	if known[addr] {
		writeError(w, http.StatusConflict, "contact_exists", contactsMessages["contact_exists"])
		return
	}
	first, last := splitName(found.Name)
	in := cardweb.Input{}
	in.First.Set, in.First.Value = true, &first
	in.Last.Set, in.Last.Value = true, &last
	emails := []cardweb.Value{{Type: "other", Value: strings.TrimSpace(found.Addr)}}
	in.Emails.Set, in.Emails.Value = true, &emails
	out, err := a.createContact(store, user, q.Book, in)
	if err != nil {
		contactsFail(w, r, q.Book, err, "contact_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
