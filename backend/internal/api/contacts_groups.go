package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Groups of Kontakte (1.13, owner 18 Nov 2026; docs/KALENDER.md "Kontakte
// 1.13"): group vCards as iOS and iCloud keep them over CardDAV
// (cardweb/groups.go). The page knows contacts by their file name; a
// group names its members by UID. These routes translate: members go out
// as file names, and come in as file names. A group stays hidden in the
// list and the search (GET /contacts) and in the routes of one contact.
// Every route: a web session of this node (pimDataArea.register).

// groupView is a group as the page shows it.
type groupView struct {
	ID   string `json:"id"`
	Book string `json:"book"`
	ETag string `json:"etag"`
	Name string `json:"name"`
	// Members are the contacts of the group (file names) in the order of
	// the card.
	Members []string `json:"members"`
	// Other counts members TitanOS cannot show: a UID of no contact here
	// (on another device's book), a mailto: member. They stay in the card.
	Other int `json:"other"`
	// ListETag: after a write, the ETag of the whole list of contacts now
	// (a group changes the book's ctag; the page keeps its list).
	ListETag string `json:"listETag,omitempty"`
	CTag     string `json:"ctag,omitempty"`
}

// bookUIDs maps the UIDs of a book's contacts to their file names (groups
// left out) and back.
type bookUIDs struct {
	byUID  map[string]string
	byName map[string]string
}

func uidsOf(objects []pimstore.ObjectInfo) bookUIDs {
	u := bookUIDs{byUID: map[string]string{}, byName: map[string]string{}}
	for _, o := range objects {
		if o.Group || o.UID == "" {
			continue
		}
		u.byUID[o.UID] = o.Name
		u.byName[o.Name] = o.UID
	}
	return u
}

// readGroup reads one group of the book with its members resolved.
func readGroup(store *pimstore.Store, user, book, name string, uids bookUIDs) (groupView, pimstore.Object, error) {
	obj, err := store.Get(pimstore.Contacts, user, book, name)
	if err != nil {
		return groupView{}, obj, err
	}
	g, err := cardweb.ReadGroup(obj.Data)
	if err != nil {
		return groupView{}, obj, err
	}
	v := groupView{ID: name, Book: book, ETag: obj.ETag, Name: g.Name, Members: []string{}, Other: g.Other}
	for _, uid := range g.UIDs {
		if id, ok := uids.byUID[uid]; ok {
			v.Members = append(v.Members, id)
		} else {
			v.Other++
		}
	}
	return v, obj, nil
}

// groupCache keeps the groups of a book under its ctag and volume, so
// the side bar (GET /contacts/overview, on every live event) reads the
// group cards from the disk only after a change – a sleeping disk is not
// woken for an unchanged book. In RAM only, a few books.
var groupCache = struct {
	sync.Mutex
	m     map[string]groupsAt
	order []string
}{m: map[string]groupsAt{}}

type groupsAt struct {
	tag    string
	groups []groupView
}

const maxCachedGroups = 32

// contactGroups lists the groups of a book, sorted by name (folded). A
// group card that cannot be read is left out (it stays on the disk).
// Shared with the later tools of the agent.
func contactGroups(store *pimstore.Store, user, book string) ([]groupView, error) {
	col, err := store.GetCollection(pimstore.Contacts, user, book)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%p\x00%s\x00%s", store, user, book)
	tag := volumeOf(store) + "\x00" + col.CTag
	groupCache.Lock()
	hit, ok := groupCache.m[key]
	groupCache.Unlock()
	if ok && hit.tag == tag {
		return slices.Clone(hit.groups), nil
	}
	groups, err := readGroups(store, user, book)
	if err != nil {
		return nil, err
	}
	groupCache.Lock()
	if _, had := groupCache.m[key]; !had {
		groupCache.order = append(groupCache.order, key)
		if len(groupCache.order) > maxCachedGroups {
			delete(groupCache.m, groupCache.order[0])
			groupCache.order = groupCache.order[1:]
		}
	}
	groupCache.m[key] = groupsAt{tag: tag, groups: groups}
	groupCache.Unlock()
	return slices.Clone(groups), nil
}

// readGroups reads the group cards of a book (contactGroups).
func readGroups(store *pimstore.Store, user, book string) ([]groupView, error) {
	objects, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		return nil, err
	}
	uids := uidsOf(objects)
	out := []groupView{}
	for _, o := range objects {
		if !o.Group {
			continue
		}
		v, _, err := readGroup(store, user, book, o.Name, uids)
		if errors.Is(err, pimstore.ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b groupView) int {
		if c := strings.Compare(cardweb.Fold(a.Name), cardweb.Fold(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// memberUIDs turns file names of contacts into their UIDs. A name of no
// contact is 404; a contact without a UID cannot be a member (400
// contacts_no_uid: its card names no UID a group could point to).
func memberUIDs(uids bookUIDs, objects []pimstore.ObjectInfo, ids []string) ([]string, string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if uid, ok := uids.byName[id]; ok {
			out = append(out, uid)
			continue
		}
		for _, o := range objects {
			if o.Name == id && !o.Group {
				return nil, "contacts_no_uid", cardweb.ErrInvalid
			}
		}
		return nil, "contact_not_found", pimstore.ErrNotFound
	}
	return out, "", nil
}

// bookParam is the address book of a request (the one, "kontakte", when
// none is named).
func bookParam(r *http.Request, given string) string {
	if given != "" {
		return given
	}
	if b := r.URL.Query().Get("book"); b != "" {
		return b
	}
	return pimstore.DefaultBook
}

type createGroupRequest struct {
	Book    string   `json:"book"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// handleCreateGroup makes a new group in iOS's form (vCard 3.0,
// X-ADDRESSBOOKSERVER-KIND:group, file <uid>.vcf); 201 with the group.
func (a pimDataArea) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	var q createGroupRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	user, book := currentUser(r).Name, bookParam(r, q.Book)
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	objects, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	uids := uidsOf(objects)
	members, code, err := memberUIDs(uids, objects, q.Members)
	if err != nil {
		groupFail(w, r, book, code, err)
		return
	}
	uid := strings.ToUpper(calweb.NewUID())
	data, err := cardweb.BuildGroup(q.Name, uid, members, a.now())
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	name := uid + ".vcf"
	if _, err := store.Put(pimstore.Contacts, user, book, name, data, pimstore.PutOptions{IfNoneMatch: true}); err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	v, _, err := readGroup(store, user, book, name, uids)
	if err != nil {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return
	}
	v.ListETag, v.CTag = a.listTagNow(store, user), a.ctagOf(store, user, book)
	writeJSON(w, http.StatusCreated, v)
}

// groupFail answers a failure with a code of the groups.
func groupFail(w http.ResponseWriter, r *http.Request, book, code string, err error) {
	if code == "" {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return
	}
	status := http.StatusNotFound
	if code == "contacts_no_uid" {
		status = http.StatusBadRequest
	}
	writeError(w, status, code, contactsMessages[code])
}

type updateGroupRequest struct {
	ETag   string   `json:"etag"`
	Name   *string  `json:"name"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// openGroup checks that name is a group of the book and reads it.
func (a pimDataArea) openGroup(w http.ResponseWriter, r *http.Request, store *pimstore.Store, user, book, name string) ([]pimstore.ObjectInfo, bookUIDs, groupView, pimstore.Object, bool) {
	fail := func(err error) ([]pimstore.ObjectInfo, bookUIDs, groupView, pimstore.Object, bool) {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return nil, bookUIDs{}, groupView{}, pimstore.Object{}, false
	}
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		return fail(err)
	}
	info, err := store.Stat(pimstore.Contacts, user, book, name)
	if err == nil && !info.Group {
		err = pimstore.ErrNotFound
	}
	if err != nil {
		return fail(err)
	}
	objects, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		return fail(err)
	}
	uids := uidsOf(objects)
	v, obj, err := readGroup(store, user, book, name, uids)
	if err != nil {
		return fail(err)
	}
	return objects, uids, v, obj, true
}

// groupConflict answers 409 contacts_conflict with the group as it is now.
func groupConflict(w http.ResponseWriter, store *pimstore.Store, user, book, name string, uids bookUIDs) {
	body := map[string]any{"error": apiError{Code: "contacts_conflict", Message: contactsMessages["contacts_conflict"]}, "current": nil}
	if v, _, err := readGroup(store, user, book, name, uids); err == nil {
		body["current"] = v
	}
	writeJSON(w, http.StatusConflict, body)
}

// handleUpdateGroup renames a group and adds or takes out members (file
// names of contacts). The etag is required; only member lines, FN/N and
// REV change (cardweb.EditGroup).
func (a pimDataArea) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	var q updateGroupRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	user, book, name := currentUser(r).Name, bookParam(r, ""), r.PathValue("id")
	objects, uids, _, obj, ok := a.openGroup(w, r, store, user, book, name)
	if !ok {
		return
	}
	if strings.TrimSpace(q.ETag) == "" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "etag is required")
		return
	}
	if !sameETag(q.ETag, obj.ETag) {
		groupConflict(w, store, user, book, name, uids)
		return
	}
	add, code, err := memberUIDs(uids, objects, q.Add)
	if err != nil {
		groupFail(w, r, book, code, err)
		return
	}
	// Taking out a contact that is gone meanwhile is no error.
	var remove []string
	for _, id := range q.Remove {
		if uid, ok := uids.byName[id]; ok {
			remove = append(remove, uid)
		}
	}
	data, changed, err := cardweb.EditGroup(obj.Data, cardweb.GroupEdit{Name: q.Name, Add: add, Remove: remove}, a.now())
	if err != nil {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return
	}
	if changed {
		_, err = store.Put(pimstore.Contacts, user, book, name, data, pimstore.PutOptions{IfMatch: obj.ETag})
		if errors.Is(err, pimstore.ErrPrecondition) {
			groupConflict(w, store, user, book, name, uids)
			return
		}
		if err != nil {
			contactsFail(w, r, book, err, "contact_group_not_found")
			return
		}
	}
	v, _, err := readGroup(store, user, book, name, uids)
	if err != nil {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return
	}
	v.ListETag, v.CTag = a.listTagNow(store, user), a.ctagOf(store, user, book)
	writeJSON(w, http.StatusOK, v)
}

// handleDeleteGroup removes a group (etag required); its contacts stay.
func (a pimDataArea) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	user, book, name := currentUser(r).Name, bookParam(r, ""), r.PathValue("id")
	_, uids, _, obj, ok := a.openGroup(w, r, store, user, book, name)
	if !ok {
		return
	}
	etag := r.URL.Query().Get("etag")
	if strings.TrimSpace(etag) == "" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "etag is required")
		return
	}
	if !sameETag(etag, obj.ETag) {
		groupConflict(w, store, user, book, name, uids)
		return
	}
	err := store.DeleteObject(pimstore.Contacts, user, book, name, obj.ETag)
	if errors.Is(err, pimstore.ErrPrecondition) {
		groupConflict(w, store, user, book, name, uids)
		return
	}
	if err != nil {
		contactsFail(w, r, book, err, "contact_group_not_found")
		return
	}
	w.Header().Set("X-List-ETag", a.listTagNow(store, user))
	w.Header().Set("X-CTag", a.ctagOf(store, user, book))
	w.WriteHeader(http.StatusNoContent)
}

// rewriteMembership moves the memberships of one contact to another in
// every group of the book (a merge: drop's UID out, keep's in). It counts
// the groups changed; a group changed meanwhile is read again once.
func (a pimDataArea) rewriteMembership(store *pimstore.Store, user, book, dropUID, keepUID string) (int, error) {
	if dropUID == "" {
		return 0, nil
	}
	objects, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range objects {
		if !o.Group {
			continue
		}
		for try := 0; try < 2; try++ {
			obj, err := store.Get(pimstore.Contacts, user, book, o.Name)
			if err != nil {
				break
			}
			g, err := cardweb.ReadGroup(obj.Data)
			if err != nil || !slices.Contains(g.UIDs, dropUID) {
				break
			}
			e := cardweb.GroupEdit{Remove: []string{dropUID}}
			if keepUID != "" {
				e.Add = []string{keepUID}
			}
			data, changed, err := cardweb.EditGroup(obj.Data, e, a.now())
			if err != nil || !changed {
				break
			}
			_, err = store.Put(pimstore.Contacts, user, book, o.Name, data, pimstore.PutOptions{IfMatch: obj.ETag})
			if errors.Is(err, pimstore.ErrPrecondition) {
				continue
			}
			if err != nil {
				return n, err
			}
			n++
			break
		}
	}
	return n, nil
}
