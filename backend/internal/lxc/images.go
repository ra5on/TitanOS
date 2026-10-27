package lxc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// indexURL lists the images the download template of LXC can fetch.
const indexURL = "https://images.linuxcontainers.org/meta/1.0/index-system"

// imageTTL is how long the list is kept in RAM before it is loaded again.
const imageTTL = 6 * time.Hour

// Image is a system image of images.linuxcontainers.org.
type Image struct {
	Distribution string `json:"distribution"`
	Release      string `json:"release"`
	Arch         string `json:"arch"`
	// Build is the date of the newest build, as the index names it.
	Build string `json:"build"`
}

type imageCache struct {
	mu      sync.Mutex
	images  []Image
	fetched time.Time
	err     error
}

func fetchIndex(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lxc: image list: %s", resp.Status)
	}
	// The list is a few dozen kilobytes; anything far larger is not it.
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// parseIndex reads lines "distribution;release;arch;variant;build;path"
// and keeps the default variant for the architecture of the NAS.
func parseIndex(data []byte, arch string) []Image {
	newest := map[string]Image{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ";")
		if len(fields) < 5 || fields[2] != arch || fields[3] != "default" {
			continue
		}
		image := Image{Distribution: fields[0], Release: fields[1], Arch: fields[2], Build: fields[4]}
		if !imageRE.MatchString(image.Distribution) || !imageRE.MatchString(image.Release) {
			continue
		}
		key := image.Distribution + "/" + image.Release
		if old, ok := newest[key]; !ok || old.Build < image.Build {
			newest[key] = image
		}
	}
	images := make([]Image, 0, len(newest))
	for _, image := range newest {
		images = append(images, image)
	}
	sort.Slice(images, func(i, j int) bool {
		if images[i].Distribution != images[j].Distribution {
			return images[i].Distribution < images[j].Distribution
		}
		return images[i].Release < images[j].Release
	})
	return images
}

// Images returns the images a container can be made of. The list is
// kept in RAM; after a failed load the last known list is returned with
// the error.
func (m *Manager) Images(ctx context.Context) ([]Image, error) {
	cache := &m.images
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.images != nil && m.now().Sub(cache.fetched) < imageTTL {
		return cache.images, nil
	}
	// A failure is not retried for a minute, so that an open page does
	// not hammer the server.
	if cache.err != nil && m.now().Sub(cache.fetched) < time.Minute {
		return cache.images, cache.err
	}
	data, err := m.fetchIndex(ctx)
	cache.fetched = m.now()
	if err == nil {
		if images := parseIndex(data, hostArch()); len(images) > 0 {
			cache.images, cache.err = images, nil
			return images, nil
		}
		err = fmt.Errorf("lxc: the image list is empty")
	}
	cache.err = err
	return cache.images, err
}
