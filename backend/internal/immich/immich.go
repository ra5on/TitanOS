// Package immich is a small client of the API of Immich, the photo app of
// the catalog (TitanOS-Apps/immich, adapted from Immich v3.3.3). TitanOS only
// reads: the account's own user, the features of the server, a search and
// the thumbnail of one asset. Every request carries the API key of one
// TitanOS account (header x-api-key); there is no key for all accounts.
//
// Field names and endpoints follow the OpenAPI document of Immich v3.3.3
// (open-api/immich-openapi-specs.json): POST /search/smart (SmartSearchDto),
// POST /search/metadata (MetadataSearchDto), GET /search/person,
// GET /assets/{id}, GET /assets/{id}/thumbnail?size=thumbnail|preview,
// GET /users/me, GET /api-keys/me, GET /server/version, GET /server/features.
package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Permissions a key needs for what TitanOS does (Immich's own names). A key
// with "all" has them; a key made for TitanOS should have only these.
var Permissions = []string{"asset.read", "asset.view", "person.read", "user.read"}

// ErrUnauthorized: Immich did not take the key (wrong, deleted, expired).
var ErrUnauthorized = errors.New("immich: the API key was not accepted")

// ErrForbidden: the key lacks a permission for this request.
var ErrForbidden = errors.New("immich: the API key lacks a permission for this")

// ErrNotFound: Immich does not know the asset (or does not show it to this key).
var ErrNotFound = errors.New("immich: not found")

// uuidRE is the form of Immich's ids (version 4 UUIDs, as in the API document).
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidID reports whether s has the form of an Immich id; nothing else is
// put into a path.
func ValidID(s string) bool { return uuidRE.MatchString(s) }

// Client talks to one Immich server with one key.
type Client struct {
	// Base is the address of the server without /api, e.g. http://127.0.0.1:2283.
	Base string
	Key  string
	HTTP *http.Client
}

// New returns a client with a short time limit; redirects are not followed,
// so the key never goes to another address.
func New(base, key string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Key: key, HTTP: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// maxJSON bounds an answer of the API that is read whole.
const maxJSON = 8 << 20

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	u := c.Base + "/api" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Key != "" {
		req.Header.Set("x-api-key", c.Key)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		// The URL error names the address, never the key (it is a header).
		return nil, fmt.Errorf("immich: %w", err)
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		res.Body.Close()
		return nil, ErrUnauthorized
	case res.StatusCode == http.StatusForbidden:
		res.Body.Close()
		return nil, ErrForbidden
	case res.StatusCode == http.StatusNotFound, res.StatusCode == http.StatusBadRequest && strings.HasPrefix(path, "/assets/"):
		res.Body.Close()
		return nil, ErrNotFound
	case res.StatusCode/100 != 2:
		res.Body.Close()
		return nil, fmt.Errorf("immich: %s %s answered %d", method, path, res.StatusCode)
	}
	return res, nil
}

func (c *Client) getJSON(ctx context.Context, method, path string, query url.Values, body, target any) error {
	res, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(io.LimitReader(res.Body, maxJSON)).Decode(target); err != nil {
		return fmt.Errorf("immich: unreadable answer to %s: %w", path, err)
	}
	return nil
}

// User is the account of the key.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Me is the user of the key.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	err := c.getJSON(ctx, http.MethodGet, "/users/me", nil, nil, &u)
	if err == nil && u.ID == "" {
		err = errors.New("immich: the answer names no user")
	}
	return u, err
}

// KeyPermissions are the permissions of the key itself (GET /api-keys/me).
func (c *Client) KeyPermissions(ctx context.Context) ([]string, error) {
	var k struct {
		Permissions []string `json:"permissions"`
	}
	err := c.getJSON(ctx, http.MethodGet, "/api-keys/me", nil, nil, &k)
	return k.Permissions, err
}

// Missing are the permissions of Permissions a key does not have.
func Missing(have []string) []string {
	if slices.Contains(have, "all") {
		return nil
	}
	var out []string
	for _, p := range Permissions {
		if !slices.Contains(have, p) {
			out = append(out, p)
		}
	}
	return out
}

// Version is the version of the server, e.g. "3.3.3".
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct{ Major, Minor, Patch int }
	if err := c.getJSON(ctx, http.MethodGet, "/server/version", nil, nil, &v); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch), nil
}

// Features are the switches of the server that matter for searching.
type Features struct {
	Search            bool `json:"search"`
	SmartSearch       bool `json:"smartSearch"`
	FacialRecognition bool `json:"facialRecognition"`
	ReverseGeocoding  bool `json:"reverseGeocoding"`
}

// Features reads what the server can do (smart search needs the
// machine-learning service of Immich).
func (c *Client) Features(ctx context.Context) (Features, error) {
	var f Features
	err := c.getJSON(ctx, http.MethodGet, "/server/features", nil, nil, &f)
	return f, err
}

// Person is a named face.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// People are the persons whose name matches (GET /search/person).
func (c *Client) People(ctx context.Context, name string) ([]Person, error) {
	var out []Person
	err := c.getJSON(ctx, http.MethodGet, "/search/person", url.Values{"name": {name}}, nil, &out)
	return out, err
}

// Exif is the part of the EXIF data TitanOS shows.
type Exif struct {
	City             string `json:"city"`
	State            string `json:"state"`
	Country          string `json:"country"`
	DateTimeOriginal string `json:"dateTimeOriginal"`
}

// Asset is a photo or video.
type Asset struct {
	ID               string `json:"id"`
	OwnerID          string `json:"ownerId"`
	Type             string `json:"type"`
	OriginalFileName string `json:"originalFileName"`
	LocalDateTime    string `json:"localDateTime"`
	FileCreatedAt    string `json:"fileCreatedAt"`
	IsTrashed        bool   `json:"isTrashed"`
	Visibility       string `json:"visibility"`
	ExifInfo         *Exif  `json:"exifInfo"`
}

// Query is a search. Text goes to the smart search (CLIP); without text,
// or when the server has no smart search, the search is by metadata.
type Query struct {
	Text      string
	PersonIDs []string
	City      string
	Country   string
	After     time.Time
	Before    time.Time
	Size      int
}

// SearchResult is one page of assets and the number of matches.
type SearchResult struct {
	Total int
	Items []Asset
	// Smart says whether the smart search answered.
	Smart bool
}

// Search runs a query: the smart search if there is text and smart is
// true, else the search by metadata.
func (c *Client) Search(ctx context.Context, q Query, smart bool) (SearchResult, error) {
	size := q.Size
	if size <= 0 || size > 100 {
		size = 24
	}
	body := map[string]any{"size": size, "withExif": true, "visibility": "timeline"}
	if len(q.PersonIDs) > 0 {
		body["personIds"] = q.PersonIDs
	}
	if q.City != "" {
		body["city"] = q.City
	}
	if q.Country != "" {
		body["country"] = q.Country
	}
	if !q.After.IsZero() {
		body["takenAfter"] = q.After.UTC().Format(time.RFC3339)
	}
	if !q.Before.IsZero() {
		body["takenBefore"] = q.Before.UTC().Format(time.RFC3339)
	}
	path := "/search/metadata"
	if smart && strings.TrimSpace(q.Text) != "" {
		path = "/search/smart"
		body["query"] = q.Text
	} else {
		body["order"] = "desc"
	}
	var answer struct {
		Assets struct {
			Total int     `json:"total"`
			Items []Asset `json:"items"`
		} `json:"assets"`
	}
	if err := c.getJSON(ctx, http.MethodPost, path, nil, body, &answer); err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Total: answer.Assets.Total, Items: answer.Assets.Items, Smart: path == "/search/smart"}, nil
}

// Asset reads one asset.
func (c *Client) Asset(ctx context.Context, id string) (Asset, error) {
	if !ValidID(id) {
		return Asset{}, ErrNotFound
	}
	var a Asset
	err := c.getJSON(ctx, http.MethodGet, "/assets/"+id, nil, nil, &a)
	return a, err
}

// Sizes of a picture TitanOS asks for.
const (
	SizeThumbnail = "thumbnail"
	SizePreview   = "preview"
)

// Thumbnail opens the picture of an asset in a size (thumbnail or
// preview). The caller closes the body.
func (c *Client) Thumbnail(ctx context.Context, id, size string) (io.ReadCloser, string, error) {
	if !ValidID(id) {
		return nil, "", ErrNotFound
	}
	if size != SizePreview {
		size = SizeThumbnail
	}
	res, err := c.do(ctx, http.MethodGet, "/assets/"+id+"/thumbnail", url.Values{"size": {size}}, nil)
	if err != nil {
		return nil, "", err
	}
	return res.Body, res.Header.Get("Content-Type"), nil
}

// TakenAt is the time an asset was taken, as Immich knows it (local time
// of the picture, without a zone), "" if unknown.
func (a Asset) TakenAt() string {
	for _, s := range []string{a.LocalDateTime, a.FileCreatedAt} {
		if s != "" {
			return s
		}
	}
	if a.ExifInfo != nil {
		return a.ExifInfo.DateTimeOriginal
	}
	return ""
}

// Place is "city, country" as far as known.
func (a Asset) Place() string {
	if a.ExifInfo == nil {
		return ""
	}
	var parts []string
	for _, s := range []string{a.ExifInfo.City, a.ExifInfo.Country} {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(parts, s) {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
