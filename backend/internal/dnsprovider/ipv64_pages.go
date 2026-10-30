package dnsprovider

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// --- status pages ---

// IPv64StatusPage is a public status page; healthchecks are IDs.
type IPv64StatusPage struct {
	ID                int        `json:"id"`
	Name              string     `json:"name"`
	Slug              string     `json:"slug"`
	URL               string     `json:"url"`
	Description       string     `json:"description"`
	PasswordProtected bool       `json:"password_protected"`
	Design            int        `json:"design"`
	Autorefresh       bool       `json:"autorefresh"`
	Healthchecks      []string   `json:"healthchecks"`
	CreatedAt         *time.Time `json:"created_at"`
	LastViewedAt      *time.Time `json:"last_viewed_at"`
}

func (p *IPv64StatusPage) hide() {
	for i, token := range p.Healthchecks {
		p.Healthchecks[i] = HealthcheckID(token)
	}
	if p.Healthchecks == nil {
		p.Healthchecks = []string{}
	}
}

// StatusPageInput is the body of creating or changing a status page; the
// password is write-only. Healthchecks are tokens here.
type StatusPageInput struct {
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Slug         *string   `json:"slug,omitempty"`
	Password     *string   `json:"password,omitempty"`
	Design       *int      `json:"design,omitempty"`
	Autorefresh  *bool     `json:"autorefresh,omitempty"`
	Healthchecks *[]string `json:"healthchecks,omitempty"`
}

var slugRE = regexp.MustCompile(`^[A-Za-z0-9]{3,128}$`)

// ValidSlug tells whether a status page address is acceptable.
func ValidSlug(slug string) bool { return slugRE.MatchString(slug) }

// Validate checks slug, design and lengths.
func (in StatusPageInput) Validate() error {
	if in.Name != nil && (len(*in.Name) > 200 || strings.ContainsAny(*in.Name, "\r\n")) {
		return invalid("name")
	}
	if in.Description != nil && len(*in.Description) > 2000 {
		return invalid("description")
	}
	if in.Slug != nil && !ValidSlug(*in.Slug) {
		return invalid("slug is 3 to 128 characters a-z, A-Z, 0-9")
	}
	if in.Password != nil && len(*in.Password) > 200 {
		return invalid("password")
	}
	if !inRange(in.Design, 1, 4) {
		return invalid("design is 1 to 4")
	}
	if in.Healthchecks != nil && len(*in.Healthchecks) > 200 {
		return invalid("healthchecks")
	}
	return nil
}

func (c *IPv64) statusPage(ctx context.Context, method, path string, body any) (IPv64StatusPage, error) {
	var p IPv64StatusPage
	err := c.call(ctx, method, path, nil, body, &p)
	p.hide()
	return p, err
}

// StatusPages lists the status pages.
func (c *IPv64) StatusPages(ctx context.Context) ([]IPv64StatusPage, error) {
	list := []IPv64StatusPage{}
	err := c.call(ctx, http.MethodGet, "/status-pages", nil, nil, &list)
	for i := range list {
		list[i].hide()
	}
	return list, err
}

// CreateStatusPage makes a status page (without slug a random address).
func (c *IPv64) CreateStatusPage(ctx context.Context, in StatusPageInput) (IPv64StatusPage, error) {
	if err := in.Validate(); err != nil {
		return IPv64StatusPage{}, err
	}
	return c.statusPage(ctx, http.MethodPost, "/status-pages", in)
}

// ChangeStatusPage changes the given fields of a status page.
func (c *IPv64) ChangeStatusPage(ctx context.Context, id int, in StatusPageInput) (IPv64StatusPage, error) {
	if err := in.Validate(); err != nil {
		return IPv64StatusPage{}, err
	}
	return c.statusPage(ctx, http.MethodPatch, "/status-pages/"+strconv.Itoa(id), in)
}

// DeleteStatusPage removes a status page.
func (c *IPv64) DeleteStatusPage(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodDelete, "/status-pages/"+strconv.Itoa(id), nil, nil, nil)
}

// --- offline pages ---

// IPv64OfflinePage is the page IPv64 shows while a failover points to it.
type IPv64OfflinePage struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Domain         string  `json:"domain"`
	Email          string  `json:"email"`
	Phone          string  `json:"phone"`
	AlternativeURL string  `json:"alternative_url"`
	Text           string  `json:"text"`
	IconURL        *string `json:"icon_url"`
	Views          int     `json:"views"`
	InUseByPolicy  *string `json:"in_use_by_policy"`
	Active         bool    `json:"active"`
	PreviewURL     string  `json:"preview_url"`
}

// OfflinePageInput is the body of creating or changing an offline page.
type OfflinePageInput struct {
	Name           *string `json:"name,omitempty"`
	Domain         *string `json:"domain,omitempty"`
	Email          *string `json:"email,omitempty"`
	Phone          *string `json:"phone,omitempty"`
	AlternativeURL *string `json:"alternative_url,omitempty"`
	Text           *string `json:"text,omitempty"`
}

var phoneRE = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

// Validate checks the formats the description gives.
func (in OfflinePageInput) Validate() error {
	if in.Name != nil && (len(*in.Name) > 200 || strings.ContainsAny(*in.Name, "\r\n")) {
		return invalid("name")
	}
	if in.Domain != nil && *in.Domain != "" && !ValidHost(*in.Domain) {
		return invalid("domain")
	}
	if in.Email != nil && *in.Email != "" && (strings.Count(*in.Email, "@") != 1 || strings.ContainsAny(*in.Email, " <>")) {
		return invalid("email")
	}
	if in.Phone != nil && *in.Phone != "" && !phoneRE.MatchString(*in.Phone) {
		return invalid("phone is digits with an optional leading +")
	}
	if in.AlternativeURL != nil && *in.AlternativeURL != "" && !httpURL(*in.AlternativeURL) {
		return invalid("alternative_url")
	}
	if in.Text != nil && len(*in.Text) > 10000 {
		return invalid("text")
	}
	return nil
}

// OfflinePages lists the offline pages.
func (c *IPv64) OfflinePages(ctx context.Context) ([]IPv64OfflinePage, error) {
	list := []IPv64OfflinePage{}
	err := c.call(ctx, http.MethodGet, "/offline-pages", nil, nil, &list)
	return list, err
}

// CreateOfflinePage makes an offline page.
func (c *IPv64) CreateOfflinePage(ctx context.Context, in OfflinePageInput) (IPv64OfflinePage, error) {
	if err := in.Validate(); err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err := c.call(ctx, http.MethodPost, "/offline-pages", nil, in, &p)
	return p, err
}

// ChangeOfflinePage changes the given fields of an offline page.
func (c *IPv64) ChangeOfflinePage(ctx context.Context, id string, in OfflinePageInput) (IPv64OfflinePage, error) {
	if !validRef(id) {
		return IPv64OfflinePage{}, invalid("offline page")
	}
	if err := in.Validate(); err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err := c.call(ctx, http.MethodPatch, "/offline-pages/"+url.PathEscape(id), nil, in, &p)
	return p, err
}

// DeleteOfflinePage removes an offline page (not while a rule uses it).
func (c *IPv64) DeleteOfflinePage(ctx context.Context, id string) error {
	if !validRef(id) {
		return invalid("offline page")
	}
	return c.call(ctx, http.MethodDelete, "/offline-pages/"+url.PathEscape(id), nil, nil, nil)
}

// MaxIconBytes and MaxIconSide are the limits of a logo.
const (
	MaxIconBytes = 1 << 20
	MaxIconSide  = 1024
)

// CheckIcon tells the media type of a logo: JPEG, PNG or WebP, at most
// 1 MB and 1024 × 1024 pixels.
func CheckIcon(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxIconBytes {
		return "", invalid("the logo is at most 1 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", invalid("the logo is JPEG, PNG or WebP")
	}
	types := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}
	kind, ok := types[format]
	if !ok {
		return "", invalid("the logo is JPEG, PNG or WebP")
	}
	if config.Width > MaxIconSide || config.Height > MaxIconSide || config.Width < 1 || config.Height < 1 {
		return "", invalid("the logo is at most 1024 × 1024 pixels")
	}
	return kind, nil
}

// UploadIcon sends a logo as multipart/form-data in the field icon.
func (c *IPv64) UploadIcon(ctx context.Context, id string, data []byte) (IPv64OfflinePage, error) {
	if !validRef(id) {
		return IPv64OfflinePage{}, invalid("offline page")
	}
	kind, err := CheckIcon(data)
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="icon"; filename="icon.`+strings.TrimPrefix(kind, "image/")+`"`)
	header.Set("Content-Type", kind)
	part, err := form.CreatePart(header)
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	part.Write(data)
	form.Close()
	answer, err := c.send(ctx, http.MethodPost, "/offline-pages/"+url.PathEscape(id)+"/icon", nil, body.Bytes(), form.FormDataContentType())
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err = decodeData(answer.Data, &p)
	return p, err
}

// DeleteIcon removes the logo of an offline page.
func (c *IPv64) DeleteIcon(ctx context.Context, id string) error {
	if !validRef(id) {
		return invalid("offline page")
	}
	return c.call(ctx, http.MethodDelete, "/offline-pages/"+url.PathEscape(id)+"/icon", nil, nil, nil)
}
