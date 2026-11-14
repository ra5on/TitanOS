// Package speech runs speech to text (faster-whisper) and text to speech
// (Piper) on the NAS (PLAN 92, docs/SPEECH.md): two containers TitanOS
// manages, reachable for Home Assistant over the Wyoming protocol and used
// by the KI-Agent. Off by default.
//
// The containers never ask the internet: titand downloads the models and
// voices (Hugging Face) with size and checksum, and the services only read
// those files. titand also hands the Wyoming connections through itself
// (proxy.go), so it decides who reaches them.
package speech

import (
	"errors"
	"regexp"
	"strings"
)

// Reach is who may connect to the Wyoming ports, which carry no password.
const (
	ReachHA  = "ha"  // the Home Assistant set up in TitanOS, and apps on this NAS
	ReachLAN = "lan" // every address of the home network
)

// DeviceCPU runs speech to text on the processor.
const DeviceCPU = "cpu"

// The ports Home Assistant expects for Wyoming services.
const (
	WhisperPort = 10300
	PiperPort   = 10200
)

// Settings are kept in /config (system/speech.json).
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volume holds the models and voices (.titanos/speech).
	Volume string `json:"volume"`
	// Device is DeviceCPU or the ID of an NVIDIA GPU (hardware list).
	Device string `json:"device"`
	// WhisperModel and Voice are the active ones; "" until one is chosen.
	WhisperModel string `json:"whisperModel"`
	// Language is what speech to text expects: "auto" or a two-letter code.
	Language string `json:"language"`
	Voice    string `json:"voice"`
	// Speed of speech: 0.5 (slow) to 2.0 (fast).
	Speed float64 `json:"speed"`
	Reach string  `json:"reach"`
	// Agent lets the KI-Agent use the services (microphone, reading aloud).
	Agent bool `json:"agent"`
}

// Defaults are the settings of a NAS that never set any.
func Defaults() Settings {
	return Settings{Device: DeviceCPU, Language: "auto", Speed: 1, Reach: ReachHA, Agent: true}
}

var (
	languageRE = regexp.MustCompile(`^[a-z]{2}$`)
	// A voice of Piper: de_DE-thorsten-high.
	voiceRE = regexp.MustCompile(`^[a-z]{2}_[A-Z]{2}-[a-z0-9_]{1,40}-(x_low|low|medium|high)$`)
	// The ID of a device as the hardware list names it (PCI address).
	deviceRE = regexp.MustCompile(`^[0-9a-zA-Z:._-]{1,64}$`)
)

// Check refuses values titand would not start a service with.
func (s *Settings) Check() error {
	if s.Device == "" {
		s.Device = DeviceCPU
	}
	if s.Language == "" {
		s.Language = "auto"
	}
	if s.Speed == 0 {
		s.Speed = 1
	}
	if s.Reach == "" {
		s.Reach = ReachHA
	}
	switch {
	case s.Device != DeviceCPU && !deviceRE.MatchString(s.Device):
		return errors.New("speech: unknown device")
	case s.WhisperModel != "" && WhisperModelByID(s.WhisperModel) == nil:
		return errors.New("speech: unknown model")
	case s.Language != "auto" && !languageRE.MatchString(s.Language):
		return errors.New("speech: the language is \"auto\" or two letters")
	case s.Voice != "" && !voiceRE.MatchString(s.Voice):
		return errors.New("speech: unknown voice")
	case s.Speed < 0.5 || s.Speed > 2:
		return errors.New("speech: the speed is between 0.5 and 2")
	case s.Reach != ReachHA && s.Reach != ReachLAN:
		return errors.New("speech: unknown reach")
	case len(s.Volume) > 64 || strings.ContainsAny(s.Volume, "/\\"):
		return errors.New("speech: unknown volume")
	}
	return nil
}

// WhisperModel is a model of faster-whisper titand can download.
type WhisperModel struct {
	ID string `json:"id"`
	// Repo is the model's repository at Hugging Face (all MIT, checked
	// 14.11.2026); titand reads its file list with sizes and SHA-256 there.
	Repo string `json:"-"`
	// Bytes is the size to expect, for the list before a download.
	Bytes int64 `json:"bytes"`
	// VRAMBytes is roughly what the model takes on a GPU (float16), from
	// other people's measurements; 0 where none were found.
	VRAMBytes int64 `json:"vramBytes,omitempty"`
	// Recommended marks the model the page suggests.
	Recommended bool `json:"recommended,omitempty"`
}

// WhisperModels is the list on offer, best first.
var WhisperModels = []WhisperModel{
	{ID: "large-v3-turbo", Repo: "dropbox-dash/faster-whisper-large-v3-turbo", Bytes: 1622 << 20, VRAMBytes: 2700 << 20, Recommended: true},
	{ID: "large-v3", Repo: "Systran/faster-whisper-large-v3", Bytes: 3091 << 20, VRAMBytes: 4400 << 20},
	{ID: "medium", Repo: "Systran/faster-whisper-medium", Bytes: 1531 << 20},
	{ID: "small", Repo: "Systran/faster-whisper-small", Bytes: 486 << 20},
	{ID: "base", Repo: "Systran/faster-whisper-base", Bytes: 148 << 20},
	{ID: "tiny", Repo: "Systran/faster-whisper-tiny", Bytes: 78 << 20},
}

// WhisperModelByID finds a model of the list.
func WhisperModelByID(id string) *WhisperModel {
	for i := range WhisperModels {
		if WhisperModels[i].ID == id {
			return &WhisperModels[i]
		}
	}
	return nil
}

// Voice is a voice of Piper as its catalog (voices.json) describes it.
type Voice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"` // de_DE
	// LanguageName in the language itself ("Deutsch").
	LanguageName string `json:"languageName"`
	Country      string `json:"country"`
	Quality      string `json:"quality"`
	Speakers     int    `json:"speakers"`
	Bytes        int64  `json:"bytes"`
	// files are the model and its description, with size and MD5.
	files []voiceFile
}

type voiceFile struct {
	path  string // in the repository: de/de_DE/thorsten/high/de_DE-thorsten-high.onnx
	bytes int64
	md5   string
}
