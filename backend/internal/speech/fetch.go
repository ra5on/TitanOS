package speech

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Models and voices come from Hugging Face: the repositories of
// faster-whisper (WhisperModels) and rhasspy/piper-voices. titand asks for
// the file list with sizes and checksums, downloads each file beside its
// place and moves it there only when size and checksum fit.

const (
	hubBase     = "https://huggingface.co"
	voicesRepo  = "rhasspy/piper-voices"
	voicesIndex = voicesRepo + "/resolve/main/voices.json"
	// maxIndex bounds the catalog of voices and a model's file list.
	maxIndex = 8 << 20
	// maxModelFile bounds one file of a model (large-v3 is 3.1 GB).
	maxModelFile = 8 << 30
)

// whisperFiles are the files of a faster-whisper model titand takes;
// anything else in a repository (README, attributes) is left there.
var whisperFiles = []string{"config.json", "model.bin", "tokenizer.json", "vocabulary.txt", "vocabulary.json", "preprocessor_config.json"}

type remoteFile struct {
	name  string // file name at its place
	url   string
	bytes int64
	// sha256 or md5 as hex; small files of a model repository have neither
	// and are checked by size only.
	sha256, md5 string
}

// hub asks Hugging Face. base and client are replaced in tests.
type hub struct {
	base   string
	client *http.Client
}

func newHub() *hub {
	return &hub{base: hubBase, client: &http.Client{Timeout: 0, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second}}}
}

func (h *hub) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TitanOS")
	res, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("speech: %s answered %d", path, res.StatusCode)
	}
	return res, nil
}

// modelFiles lists the files of a faster-whisper model.
func (h *hub) modelFiles(ctx context.Context, m WhisperModel) ([]remoteFile, error) {
	res, err := h.get(ctx, "api/models/"+m.Repo+"?blobs=true")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var info struct {
		Siblings []struct {
			Name string `json:"rfilename"`
			Size int64  `json:"size"`
			LFS  *struct {
				SHA256 string `json:"sha256"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxIndex)).Decode(&info); err != nil {
		return nil, fmt.Errorf("speech: file list of %s: %w", m.ID, err)
	}
	var files []remoteFile
	hasModel := false
	for _, s := range info.Siblings {
		if !contains(whisperFiles, s.Name) || s.Size <= 0 || s.Size > maxModelFile {
			continue
		}
		f := remoteFile{name: s.Name, url: m.Repo + "/resolve/main/" + s.Name, bytes: s.Size}
		if s.LFS != nil {
			f.sha256 = strings.ToLower(s.LFS.SHA256)
		}
		if s.Name == "model.bin" {
			// The model itself is never taken without its checksum.
			if len(f.sha256) != 64 {
				return nil, fmt.Errorf("speech: %s names no checksum for its model", m.ID)
			}
			hasModel = true
		}
		files = append(files, f)
	}
	if !hasModel || len(files) < 3 {
		return nil, fmt.Errorf("speech: %s is not a complete model", m.ID)
	}
	return files, nil
}

// voices reads the catalog of Piper voices; raw is what was read, to keep.
func (h *hub) voices(ctx context.Context) ([]Voice, []byte, error) {
	res, err := h.get(ctx, voicesIndex)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxIndex+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > maxIndex {
		return nil, nil, errors.New("speech: the catalog of voices is too large")
	}
	list, err := parseVoices(raw)
	return list, raw, err
}

// parseVoices reads voices.json of rhasspy/piper-voices.
func parseVoices(raw []byte) ([]Voice, error) {
	var index map[string]struct {
		Name     string `json:"name"`
		Language struct {
			Code    string `json:"code"`
			Native  string `json:"name_native"`
			Country string `json:"country_english"`
		} `json:"language"`
		Quality  string `json:"quality"`
		Speakers int    `json:"num_speakers"`
		Files    map[string]struct {
			Size int64  `json:"size_bytes"`
			MD5  string `json:"md5_digest"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, fmt.Errorf("speech: catalog of voices: %w", err)
	}
	var out []Voice
	for id, v := range index {
		if !voiceRE.MatchString(id) {
			continue
		}
		voice := Voice{ID: id, Name: v.Name, Language: v.Language.Code, LanguageName: v.Language.Native, Country: v.Language.Country, Quality: v.Quality, Speakers: v.Speakers}
		for path, f := range v.Files {
			// Only the model and its description, at the names the voice has.
			name := path[strings.LastIndex(path, "/")+1:]
			if name != id+".onnx" && name != id+".onnx.json" {
				continue
			}
			if strings.Contains(path, "..") || f.Size <= 0 || f.Size > maxModelFile || len(f.MD5) != 32 {
				continue
			}
			voice.files = append(voice.files, voiceFile{path: path, bytes: f.Size, md5: strings.ToLower(f.MD5)})
			voice.Bytes += f.Size
		}
		if len(voice.files) == 2 {
			out = append(out, voice)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) == 0 {
		return nil, errors.New("speech: the catalog of voices is empty")
	}
	return out, nil
}

func (v Voice) remote() []remoteFile {
	var files []remoteFile
	for _, f := range v.files {
		files = append(files, remoteFile{name: f.path[strings.LastIndex(f.path, "/")+1:], url: voicesRepo + "/resolve/main/" + f.path, bytes: f.bytes, md5: f.md5})
	}
	return files
}

// download fetches one file into dir of root as name: written beside its
// place and moved there once size and checksum fit. step reports the
// bytes read since the last call.
func (h *hub) download(ctx context.Context, root *os.Root, dir string, f remoteFile, step func(n int64)) error {
	res, err := h.get(ctx, f.url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	part := dir + "/." + f.name + ".part"
	out, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		out.Close()
		if !keep {
			root.Remove(part)
		}
	}()
	var sum hash.Hash
	want := ""
	switch {
	case f.sha256 != "":
		sum, want = sha256.New(), f.sha256
	case f.md5 != "":
		sum, want = md5.New(), f.md5
	}
	var w io.Writer = out
	if sum != nil {
		w = io.MultiWriter(out, sum)
	}
	// One byte more than expected shows a file that grew.
	n, err := io.Copy(w, &countReader{r: io.LimitReader(res.Body, f.bytes+1), step: step})
	if err != nil {
		return err
	}
	if n != f.bytes {
		return fmt.Errorf("speech: %s has %d bytes, %d were expected", f.name, n, f.bytes)
	}
	if sum != nil && hex.EncodeToString(sum.Sum(nil)) != want {
		return fmt.Errorf("speech: the checksum of %s does not fit", f.name)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	keep = true
	return root.Rename(part, dir+"/"+f.name)
}

type countReader struct {
	r    io.Reader
	step func(n int64)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.step != nil {
		c.step(int64(n))
	}
	return n, err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
