package catalog

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	// maxDownload bounds a file that has to be held in memory: a zip
	// archive or a single compose file.
	maxDownload = 64 << 20
	// maxArchive bounds a tar.gz archive, which is read as it arrives.
	// Stores with screenshots of every app are several hundred MiB.
	maxArchive  = 1 << 30
	maxUnpacked = 8 << 30
	// maxTemplates and maxTemplateBytes bound what is kept in memory.
	maxTemplates     = 3000
	maxTemplateBytes = 256 << 10
	// fetchTimeout bounds one fetch of a source, however large.
	fetchTimeout = 20 * time.Minute
)

// errNotModified means the source is still what it was at the last fetch.
var errNotModified = errors.New("catalog: not modified")

// newClient returns the HTTP client sources are fetched with. A fetch may
// take minutes; only a server that does not answer at all is given up on.
func newClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}

// download returns the YAML files of a source by their path, and the
// version the server gave them. With the version of the last fetch, a
// server that knows it answers errNotModified instead of sending it all again.
//
// An address ending in .yaml is a single compose file, one ending in .zip or
// .tar.gz an archive. Everything else is taken for a Git repository on a
// forge, which hands out its files as an archive without Git.
func download(ctx context.Context, client *http.Client, address, etag string) (map[string]string, string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, "", ErrInvalidURL
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	name := strings.ToLower(parsed.Path)
	candidates := []string{address}
	kind := "tar"
	switch {
	case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
		kind = "yaml"
	case strings.HasSuffix(name, ".zip"):
		kind = "zip"
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
	default:
		base := strings.TrimSuffix(strings.TrimSuffix(address, "/"), ".git")
		// GitHub, Gitea and Forgejo; then GitLab.
		candidates = []string{base + "/archive/HEAD.tar.gz", base + "/-/archive/HEAD/archive.tar.gz"}
	}

	for _, candidate := range candidates {
		var files map[string]string
		var version string
		files, version, err = fetch(ctx, client, candidate, kind, parsed.Path, etag)
		if err == nil || errors.Is(err, errNotModified) {
			return files, version, err
		}
	}
	return nil, "", err
}

func fetch(ctx context.Context, client *http.Client, address, kind, name, etag string) (map[string]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, "", ErrInvalidURL
	}
	req.Header.Set("User-Agent", "TitanOS")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return nil, etag, errNotModified
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: %s answered %s", ErrUnreachable, address, res.Status)
	}
	version := res.Header.Get("ETag")

	if kind == "tar" {
		files, err := unpackTar(io.LimitReader(res.Body, maxArchive))
		return files, version, err
	}
	limit := int64(maxDownload)
	if kind == "yaml" {
		limit = maxTemplateBytes
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("%w: %s is larger than %d MiB", ErrUnreachable, address, limit>>20)
	}
	if kind == "yaml" {
		return map[string]string{name: string(data)}, version, nil
	}
	files, err := unpackZip(data)
	return files, version, err
}

// isTemplate reports whether a file in an archive could be a compose file.
func isTemplate(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}
	// .github/workflows and the like.
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return false
		}
	}
	return true
}

// keep stores a file of an archive if it could be a compose file.
func keep(files map[string]string, name string, r io.Reader) {
	if len(files) >= maxTemplates || !isTemplate(name) {
		return
	}
	content, err := io.ReadAll(io.LimitReader(r, maxTemplateBytes+1))
	if err == nil && len(content) <= maxTemplateBytes {
		files[name] = string(content)
	}
}

// unpackZip reads the YAML files out of a zip archive.
func unpackZip(data []byte) (map[string]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || file.UncompressedSize64 > maxTemplateBytes || !isTemplate(file.Name) {
			continue
		}
		if r, err := file.Open(); err == nil {
			keep(files, file.Name, r)
			r.Close()
		}
	}
	return files, nil
}

// unpackTar reads the YAML files out of a tar.gz archive as it arrives;
// everything else in it is skipped without being kept.
func unpackTar(r io.Reader) (map[string]string, error) {
	unzipped, err := gzip.NewReader(r)
	if err != nil {
		// A web page instead of an archive, most likely.
		return nil, fmt.Errorf("%w: not an archive", ErrUnreachable)
	}
	files := map[string]string{}
	archive := tar.NewReader(io.LimitReader(unzipped, maxUnpacked))
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		if header.Typeflag == tar.TypeReg && header.Size <= maxTemplateBytes {
			keep(files, header.Name, archive)
		}
	}
	return files, nil
}
