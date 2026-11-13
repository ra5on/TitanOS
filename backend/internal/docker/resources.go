package docker

// Images, volumes and the build cache of the engine (PLAN 67-1, Docker ›
// Images, Volumes, Aufräumen). titand decides itself what may go: the
// engine's own prune endpoints for images and volumes are never called,
// because "all unused" would also take the images of the remote view and
// of the agent's sandbox. Everything is deleted one by one and never by
// force, so the engine still refuses what a container uses.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInUse refuses an image or volume a container (running or
	// stopped) uses.
	ErrInUse = errors.New("docker: in use by a container")
	// ErrProtected refuses an image or volume TitanOS itself needs.
	ErrProtected = errors.New("docker: belongs to TitanOS")
	// errAppWork refuses cleaning up while an app is installed or updated:
	// an install pulls its images before it creates the containers, and an
	// update keeps the previous images for going back.
	errAppWork = fmt.Errorf("%w: an app is being installed or updated", ErrBusy)
)

const (
	// dfTimeout bounds the engine's disk usage count, which walks every
	// volume and can take long on a full disk. It runs in the background,
	// independent of the request that started it.
	dfTimeout = 10 * time.Minute
	// dfMaxAge is how long a count is reused; deleting something drops it.
	dfMaxAge = 2 * time.Minute
	// composeProjectLabel names the Compose project (= TitanOS app).
	composeProjectLabel = "com.docker.compose.project"
	// anonymousVolumeLabel marks volumes the engine created without a
	// name (VOLUME of an image, `-v /path`), since Docker 23.
	anonymousVolumeLabel = "com.docker.volume.anonymous"
)

var (
	imageRefRE   = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)
	volumeNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
	hexNameRE    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ResourceUser is a container that uses an image or a volume.
type ResourceUser struct {
	// App is the TitanOS app (Compose project); empty for containers
	// outside an app.
	App       string `json:"app"`
	Container string `json:"container"`
	// Running tells running and stopped containers apart.
	Running bool `json:"running"`
}

// Image is an image of the engine for Docker › Images.
type Image struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repoTags"`
	// Size includes the layers shared with other images; SharedSize is
	// that shared part (-1 unknown). Deleting frees about Size-SharedSize.
	Size       int64          `json:"size"`
	SharedSize int64          `json:"sharedSize"`
	Created    time.Time      `json:"created"`
	UsedBy     []ResourceUser `json:"usedBy"`
	Dangling   bool           `json:"dangling"`
	// Protected names why TitanOS keeps the image ("viewer": remote view,
	// "sandbox": the agent's sandbox); empty for images that may go.
	Protected string `json:"protected,omitempty"`
}

// ImageTotals sums up the list.
type ImageTotals struct {
	Count int `json:"count"`
	// Size adds up the sizes of all images; layers shared between images
	// count once per image.
	Size int64 `json:"size"`
	// UnusedCount/UnusedSize are the images "remove unused" would take
	// (not used, not protected); DanglingCount/DanglingSize the untagged
	// ones among them. The sizes are what about would be freed.
	UnusedCount   int   `json:"unusedCount"`
	UnusedSize    int64 `json:"unusedSize"`
	DanglingCount int   `json:"danglingCount"`
	DanglingSize  int64 `json:"danglingSize"`
}

// ImageList is the answer of GET /api/v1/docker/images.
type ImageList struct {
	Images []Image     `json:"images"`
	Totals ImageTotals `json:"totals"`
}

// Volume is a volume of the engine for Docker › Volumes.
type Volume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	// Size in bytes from the engine's disk usage count, -1 while unknown
	// (count still running, timed out, or a driver that cannot tell).
	Size      int64             `json:"size"`
	UsedBy    []ResourceUser    `json:"usedBy"`
	Labels    map[string]string `json:"labels"`
	CreatedAt string            `json:"createdAt"`
	// App is the Compose project the volume was made for (may be an app
	// that is saved but has no containers at the moment).
	App       string `json:"app,omitempty"`
	Anonymous bool   `json:"anonymous"`
	// TitanOS marks volumes TitanOS itself manages; they are never deleted
	// here.
	TitanOS bool `json:"titanos"`
}

// PruneFailure is an item a cleanup could not delete.
type PruneFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// PruneResult is the answer of the cleanups.
type PruneResult struct {
	Deleted []string       `json:"deleted"`
	Failed  []PruneFailure `json:"failed"`
	// Reclaimed is about the space freed (bytes): for images the size
	// without shared layers, for volumes the last known size.
	Reclaimed int64 `json:"reclaimed"`
}

// DiskUsagePart is one line of the cleanup page.
type DiskUsagePart struct {
	Count int   `json:"count"`
	Size  int64 `json:"size"`
	// Reclaimable is what the cleanups on the page could free.
	Reclaimable int64 `json:"reclaimable"`
}

// DiskUsage is the answer of GET /api/v1/docker/df.
type DiskUsage struct {
	Images DiskUsagePart `json:"images"`
	// Containers: the writable layers; Reclaimable those of stopped
	// containers (removed in Docker › Container, not on the cleanup page).
	Containers DiskUsagePart `json:"containers"`
	Volumes    DiskUsagePart `json:"volumes"`
	// AnonymousReclaimable is the part of Volumes.Reclaimable in unused
	// anonymous volumes (the default of the volume cleanup).
	AnonymousReclaimable int64         `json:"anonymousReclaimable"`
	BuildCache           DiskUsagePart `json:"buildCache"`
	// Reclaimable adds up images, volumes and build cache.
	Reclaimable int64     `json:"reclaimable"`
	MeasuredAt  time.Time `json:"measuredAt"`
	// Measuring: a count is running; without a previous one all values
	// are 0 and MeasuredAt is zero, so ask again a little later.
	Measuring bool `json:"measuring"`
}

// rawContainer is GET /containers/json without the filter of containers():
// viewers and sandboxes use images and volumes too.
type rawContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Labels  map[string]string `json:"Labels"`
	Mounts  []struct {
		Type string `json:"Type"`
		Name string `json:"Name"`
	} `json:"Mounts"`
}

type rawImage struct {
	ID          string            `json:"Id"`
	RepoTags    []string          `json:"RepoTags"`
	RepoDigests []string          `json:"RepoDigests"`
	Created     int64             `json:"Created"`
	Size        int64             `json:"Size"`
	SharedSize  int64             `json:"SharedSize"`
	Containers  int64             `json:"Containers"`
	Labels      map[string]string `json:"Labels"`
}

type rawVolume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels"`
	UsageData  *struct {
		Size     int64 `json:"Size"`
		RefCount int64 `json:"RefCount"`
	} `json:"UsageData"`
}

type rawDF struct {
	LayersSize int64      `json:"LayersSize"`
	Images     []rawImage `json:"Images"`
	Containers []struct {
		State  string `json:"State"`
		SizeRw int64  `json:"SizeRw"`
	} `json:"Containers"`
	Volumes    []rawVolume `json:"Volumes"`
	BuildCache []struct {
		Size   int64 `json:"Size"`
		InUse  bool  `json:"InUse"`
		Shared bool  `json:"Shared"`
	} `json:"BuildCache"`
}

// dfCache runs the engine's disk usage count once at a time in the
// background (the engine refuses a second one while one runs) and keeps
// the answer for a while. mu is never held across the engine call.
type dfCache struct {
	mu    sync.Mutex
	value *rawDF
	at    time.Time
	// gen counts changes; a count started before one is not kept.
	gen int
	// running is closed when the running count ends; nil when none runs.
	running chan struct{}
	lastErr error
}

// dfWaitList is how long the volume list waits for a count (sizes are -1
// after that); dfWaitPage how long GET /docker/df waits. Variables for
// tests.
var (
	dfWaitList = 3 * time.Second
	dfWaitPage = 30 * time.Second
)

// errMeasuring: the count is still running.
var errMeasuring = errors.New("docker: disk usage is still being counted")

// ready refuses while Docker is off, in development mode or not started.
func (m *Manager) ready() error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	applied := m.applied
	m.mu.Unlock()
	if applied == "" {
		return ErrNotRunning
	}
	return nil
}

// cleanable refuses changes while app work owns the images and volumes.
func (m *Manager) cleanable() error {
	if err := m.ready(); err != nil {
		return err
	}
	if m.backupActive() {
		return errBackupRunning
	}
	if m.Busy() {
		return errAppWork
	}
	return nil
}

func (m *Manager) allContainers(ctx context.Context) ([]rawContainer, error) {
	var list []rawContainer
	if err := m.api.getJSON(ctx, "/containers/json", url.Values{"all": {"true"}}, &list); err != nil {
		return nil, mapAPIError(err)
	}
	return list, nil
}

func userOf(c rawContainer) ResourceUser {
	name := c.ID
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	app := c.Labels[composeProjectLabel]
	if c.Labels[ViewerLabel] != "" || c.Labels[sandboxLabel] != "" || c.Labels[NVRLabel] != "" {
		app = "" // viewers, sandboxes and the NVR are no apps
	}
	return ResourceUser{App: app, Container: name, Running: c.State == "running"}
}

func realTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		if t != "" && t != "<none>:<none>" {
			out = append(out, t)
		}
	}
	return out
}

// imageProtection says why TitanOS keeps an image: the remote view's own
// images and the pinned image of the agent's sandbox.
func imageProtection(img rawImage) string {
	if ownImage(img.Labels) {
		return "viewer"
	}
	_, sandboxDigest, _ := strings.Cut(SandboxImage, "@")
	for _, d := range img.RepoDigests {
		if _, digest, ok := strings.Cut(d, "@"); ok && digest == sandboxDigest {
			return "sandbox"
		}
	}
	return ""
}

// freed is about what deleting an image frees.
func freed(size, shared int64) int64 {
	if shared > 0 && shared <= size {
		return size - shared
	}
	return size
}

// Images lists the images with who uses them.
func (m *Manager) Images(ctx context.Context) (ImageList, error) {
	if err := m.ready(); err != nil {
		return ImageList{}, err
	}
	images, users, err := m.imagesWithUsers(ctx)
	if err != nil {
		return ImageList{}, err
	}
	out := ImageList{Images: []Image{}}
	for _, img := range images {
		tags := realTags(img.RepoTags)
		item := Image{ID: img.ID, RepoTags: tags, Size: img.Size, SharedSize: img.SharedSize, Created: time.Unix(img.Created, 0).UTC(),
			UsedBy: users[img.ID], Dangling: len(tags) == 0, Protected: imageProtection(img)}
		if item.UsedBy == nil {
			item.UsedBy = []ResourceUser{}
		}
		out.Images = append(out.Images, item)
		out.Totals.Count++
		out.Totals.Size += img.Size
		if len(item.UsedBy) == 0 && item.Protected == "" {
			out.Totals.UnusedCount++
			out.Totals.UnusedSize += freed(img.Size, img.SharedSize)
			if item.Dangling {
				out.Totals.DanglingCount++
				out.Totals.DanglingSize += freed(img.Size, img.SharedSize)
			}
		}
	}
	sort.Slice(out.Images, func(i, j int) bool {
		a, b := out.Images[i], out.Images[j]
		if a.Dangling != b.Dangling {
			return !a.Dangling
		}
		return strings.Join(a.RepoTags, ",")+a.ID < strings.Join(b.RepoTags, ",")+b.ID
	})
	return out, nil
}

func (m *Manager) imagesWithUsers(ctx context.Context) ([]rawImage, map[string][]ResourceUser, error) {
	var images []rawImage
	if err := m.api.getJSON(ctx, "/images/json", url.Values{"shared-size": {"true"}}, &images); err != nil {
		return nil, nil, mapAPIError(err)
	}
	containers, err := m.allContainers(ctx)
	if err != nil {
		return nil, nil, err
	}
	users := map[string][]ResourceUser{}
	for _, c := range containers {
		users[c.ImageID] = append(users[c.ImageID], userOf(c))
	}
	return images, users, nil
}

// findImage resolves an ID (with or without sha256:, full or at least 12
// characters) to exactly one image.
func findImage(images []rawImage, ref string) (rawImage, bool) {
	ref = strings.TrimPrefix(ref, "sha256:")
	var found []rawImage
	for _, img := range images {
		if strings.HasPrefix(strings.TrimPrefix(img.ID, "sha256:"), ref) {
			found = append(found, img)
		}
	}
	if len(found) != 1 {
		return rawImage{}, false
	}
	return found[0], true
}

// RemoveImage deletes an image no container uses. It is never forced.
func (m *Manager) RemoveImage(ctx context.Context, ref string) (Image, error) {
	if !imageRefRE.MatchString(ref) {
		return Image{}, ErrNotFound
	}
	if err := m.cleanable(); err != nil {
		return Image{}, err
	}
	images, users, err := m.imagesWithUsers(ctx)
	if err != nil {
		return Image{}, err
	}
	img, ok := findImage(images, ref)
	if !ok {
		return Image{}, ErrNotFound
	}
	item := Image{ID: img.ID, RepoTags: realTags(img.RepoTags), Size: img.Size, SharedSize: img.SharedSize, UsedBy: users[img.ID], Protected: imageProtection(img)}
	switch {
	case item.Protected != "":
		return item, ErrProtected
	case len(item.UsedBy) > 0:
		return item, ErrInUse
	}
	defer m.dropDF()
	return item, m.deleteImage(ctx, img)
}

// deleteImage removes an image without force. The engine refuses to
// delete an image with several names by ID, so all names but one are
// taken off first; then the image goes by ID.
func (m *Manager) deleteImage(ctx context.Context, img rawImage) error {
	tags := realTags(img.RepoTags)
	for i := 1; i < len(tags); i++ {
		if err := m.deleteImageRef(ctx, tags[i]); err != nil {
			return err
		}
	}
	return m.deleteImageRef(ctx, img.ID)
}

func (m *Manager) deleteImageRef(ctx context.Context, ref string) error {
	res, err := m.api.do(ctx, http.MethodDelete, "/images/"+url.PathEscape(ref), nil)
	if err == nil {
		res.Body.Close()
		return nil
	}
	var api *apiError
	if errors.As(err, &api) && api.Status == http.StatusConflict {
		return fmt.Errorf("%w: %s", ErrInUse, api.Message)
	}
	if notFound(err) {
		return nil
	}
	return mapAPIError(err)
}

// PruneImages deletes the images no container uses that TitanOS does not
// keep: only untagged ones, or with unused all unused ones.
func (m *Manager) PruneImages(ctx context.Context, unused bool) (PruneResult, error) {
	result := PruneResult{Deleted: []string{}, Failed: []PruneFailure{}}
	if err := m.cleanable(); err != nil {
		return result, err
	}
	images, users, err := m.imagesWithUsers(ctx)
	if err != nil {
		return result, err
	}
	defer m.dropDF()
	for _, img := range images {
		dangling := len(realTags(img.RepoTags)) == 0
		if len(users[img.ID]) > 0 || imageProtection(img) != "" || (!unused && !dangling) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := m.deleteImage(ctx, img); err != nil {
			result.Failed = append(result.Failed, PruneFailure{ID: img.ID, Error: err.Error()})
			continue
		}
		result.Deleted = append(result.Deleted, img.ID)
		result.Reclaimed += freed(img.Size, img.SharedSize)
	}
	return result, nil
}

// titanosVolume marks volumes of TitanOS itself: a label titanos.… or a
// name titanos-…. TitanOS creates no named volumes today (the remote view
// and the sandbox use folders), so this guards future ones.
func titanosVolume(v rawVolume) bool {
	if strings.HasPrefix(v.Name, "titanos-") || strings.HasPrefix(v.Name, "titanos_") {
		return true
	}
	for key := range v.Labels {
		if strings.HasPrefix(key, "titanos.") {
			return true
		}
	}
	return false
}

func anonymousVolume(v rawVolume) bool {
	_, labelled := v.Labels[anonymousVolumeLabel]
	return labelled || (hexNameRE.MatchString(v.Name) && v.Labels[composeProjectLabel] == "")
}

// Volumes lists the volumes with who uses them and their size.
func (m *Manager) Volumes(ctx context.Context) ([]Volume, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	volumes, users, err := m.volumesWithUsers(ctx)
	if err != nil {
		return nil, err
	}
	sizes := m.volumeSizes(ctx)
	out := make([]Volume, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, volumeOf(v, users, sizes))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Anonymous != out[j].Anonymous {
			return !out[i].Anonymous
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func volumeOf(v rawVolume, users map[string][]ResourceUser, sizes map[string]int64) Volume {
	item := Volume{Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Size: -1, UsedBy: users[v.Name], Labels: v.Labels,
		CreatedAt: v.CreatedAt, App: v.Labels[composeProjectLabel], Anonymous: anonymousVolume(v), TitanOS: titanosVolume(v)}
	if size, ok := sizes[v.Name]; ok {
		item.Size = size
	}
	if item.UsedBy == nil {
		item.UsedBy = []ResourceUser{}
	}
	if item.Labels == nil {
		item.Labels = map[string]string{}
	}
	return item
}

func (m *Manager) volumesWithUsers(ctx context.Context) ([]rawVolume, map[string][]ResourceUser, error) {
	var answer struct {
		Volumes []rawVolume `json:"Volumes"`
	}
	if err := m.api.getJSON(ctx, "/volumes", nil, &answer); err != nil {
		return nil, nil, mapAPIError(err)
	}
	containers, err := m.allContainers(ctx)
	if err != nil {
		return nil, nil, err
	}
	users := map[string][]ResourceUser{}
	for _, c := range containers {
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name != "" {
				users[mount.Name] = append(users[mount.Name], userOf(c))
			}
		}
	}
	return answer.Volumes, users, nil
}

// RemoveVolume deletes a volume no container uses. Its data is lost; the
// UI asks first. Volumes of TitanOS are refused.
func (m *Manager) RemoveVolume(ctx context.Context, name string) error {
	if !volumeNameRE.MatchString(name) {
		return ErrNotFound
	}
	if err := m.cleanable(); err != nil {
		return err
	}
	volumes, users, err := m.volumesWithUsers(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(volumes, func(v rawVolume) bool { return v.Name == name })
	switch {
	case i < 0:
		return ErrNotFound
	case titanosVolume(volumes[i]):
		return ErrProtected
	case len(users[name]) > 0:
		return ErrInUse
	}
	defer m.dropDF()
	return m.deleteVolume(ctx, name)
}

func (m *Manager) deleteVolume(ctx context.Context, name string) error {
	res, err := m.api.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name), nil)
	if err == nil {
		res.Body.Close()
		return nil
	}
	var api *apiError
	if errors.As(err, &api) && api.Status == http.StatusConflict {
		return fmt.Errorf("%w: %s", ErrInUse, api.Message)
	}
	if notFound(err) {
		return nil
	}
	return mapAPIError(err)
}

// PruneVolumes deletes unused volumes: only anonymous ones, or with
// anonymousOnly false also named ones. Volumes of TitanOS and of apps that
// are still saved (an app taken down keeps its data) always stay.
func (m *Manager) PruneVolumes(ctx context.Context, anonymousOnly bool) (PruneResult, error) {
	result := PruneResult{Deleted: []string{}, Failed: []PruneFailure{}}
	if err := m.cleanable(); err != nil {
		return result, err
	}
	volumes, users, err := m.volumesWithUsers(ctx)
	if err != nil {
		return result, err
	}
	m.mu.Lock()
	apps := m.knownApps()
	m.mu.Unlock()
	sizes := m.volumeSizes(ctx)
	defer m.dropDF()
	for _, v := range volumes {
		if len(users[v.Name]) > 0 || titanosVolume(v) || (anonymousOnly && !anonymousVolume(v)) {
			continue
		}
		if app := v.Labels[composeProjectLabel]; app != "" && slices.Contains(apps, app) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := m.deleteVolume(ctx, v.Name); err != nil {
			result.Failed = append(result.Failed, PruneFailure{ID: v.Name, Error: err.Error()})
			continue
		}
		result.Deleted = append(result.Deleted, v.Name)
		if size := sizes[v.Name]; size > 0 {
			result.Reclaimed += size
		}
	}
	return result, nil
}

// PruneBuildCache empties the build cache (what a build uses right now
// stays).
func (m *Manager) PruneBuildCache(ctx context.Context) (PruneResult, error) {
	result := PruneResult{Deleted: []string{}, Failed: []PruneFailure{}}
	if err := m.cleanable(); err != nil {
		return result, err
	}
	var answer struct {
		CachesDeleted  []string `json:"CachesDeleted"`
		SpaceReclaimed int64    `json:"SpaceReclaimed"`
	}
	defer m.dropDF()
	res, err := m.api.do(ctx, http.MethodPost, "/build/prune", url.Values{"all": {"true"}})
	if err != nil {
		return result, mapAPIError(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&answer); err != nil {
		return result, err
	}
	if answer.CachesDeleted != nil {
		result.Deleted = answer.CachesDeleted
	}
	result.Reclaimed = answer.SpaceReclaimed
	return result, nil
}

// dropDF forgets the last disk usage count after a change; a count that
// runs right now is not kept either.
func (m *Manager) dropDF() {
	m.df.mu.Lock()
	m.df.value = nil
	m.df.gen++
	m.df.mu.Unlock()
}

// startDFLocked starts a count unless one runs; m.df.mu is held.
func (m *Manager) startDFLocked() chan struct{} {
	if m.df.running != nil {
		return m.df.running
	}
	done := make(chan struct{})
	m.df.running = done
	gen := m.df.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), dfTimeout)
		defer cancel()
		var df rawDF
		err := m.api.getJSON(ctx, "/system/df", nil, &df)
		m.df.mu.Lock()
		defer m.df.mu.Unlock()
		m.df.lastErr = mapAPIError(err)
		if err == nil && gen == m.df.gen {
			m.df.value, m.df.at = &df, time.Now()
		}
		m.df.running = nil
		close(done)
	}()
	return done
}

// diskUsage returns the engine's disk usage count. A kept one comes at
// once (an old one is renewed in the background); otherwise it waits up
// to wait for the count, then errMeasuring. fresh starts a new count.
func (m *Manager) diskUsage(ctx context.Context, wait time.Duration, fresh bool) (*rawDF, error) {
	m.df.mu.Lock()
	if fresh {
		m.df.value = nil
	}
	if value := m.df.value; value != nil {
		if time.Since(m.df.at) >= dfMaxAge {
			m.startDFLocked()
		}
		m.df.mu.Unlock()
		return value, nil
	}
	done := m.startDFLocked()
	m.df.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return nil, errMeasuring
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	m.df.mu.Lock()
	defer m.df.mu.Unlock()
	if m.df.value != nil {
		return m.df.value, nil
	}
	if m.df.lastErr != nil {
		return nil, m.df.lastErr
	}
	return nil, errMeasuring // dropped by a change meanwhile
}

// volumeSizes are the sizes of the last count, waiting at most a moment.
func (m *Manager) volumeSizes(ctx context.Context) map[string]int64 {
	sizes := map[string]int64{}
	if df, err := m.diskUsage(ctx, dfWaitList, false); err == nil {
		for _, v := range df.Volumes {
			if v.UsageData != nil {
				sizes[v.Name] = v.UsageData.Size
			}
		}
	}
	return sizes
}

// DiskUsage sums up what Docker takes on the volume and what the cleanups
// could free.
func (m *Manager) DiskUsage(ctx context.Context, fresh bool) (DiskUsage, error) {
	if err := m.ready(); err != nil {
		return DiskUsage{}, err
	}
	df, err := m.diskUsage(ctx, dfWaitPage, fresh)
	if errors.Is(err, errMeasuring) {
		return DiskUsage{Measuring: true}, nil
	}
	if err != nil {
		return DiskUsage{}, err
	}
	m.df.mu.Lock()
	at, measuring := m.df.at, m.df.running != nil
	m.df.mu.Unlock()
	m.mu.Lock()
	apps := m.knownApps()
	m.mu.Unlock()
	var out DiskUsage
	out.MeasuredAt, out.Measuring = at.UTC(), measuring
	out.Images.Count = len(df.Images)
	out.Images.Size = df.LayersSize
	for _, img := range df.Images {
		if img.Containers == 0 && imageProtection(img) == "" {
			out.Images.Reclaimable += freed(img.Size, img.SharedSize)
		}
	}
	out.Containers.Count = len(df.Containers)
	for _, c := range df.Containers {
		out.Containers.Size += c.SizeRw
		if c.State != "running" {
			out.Containers.Reclaimable += c.SizeRw
		}
	}
	out.Volumes.Count = len(df.Volumes)
	for _, v := range df.Volumes {
		if v.UsageData == nil || v.UsageData.Size < 0 {
			continue
		}
		out.Volumes.Size += v.UsageData.Size
		app := v.Labels[composeProjectLabel]
		if v.UsageData.RefCount == 0 && !titanosVolume(v) && (app == "" || !slices.Contains(apps, app)) {
			out.Volumes.Reclaimable += v.UsageData.Size
			if anonymousVolume(v) {
				out.AnonymousReclaimable += v.UsageData.Size
			}
		}
	}
	out.BuildCache.Count = len(df.BuildCache)
	for _, b := range df.BuildCache {
		out.BuildCache.Size += b.Size
		if !b.InUse && !b.Shared {
			out.BuildCache.Reclaimable += b.Size
		}
	}
	out.Reclaimable = out.Images.Reclaimable + out.Volumes.Reclaimable + out.BuildCache.Reclaimable
	return out, nil
}
