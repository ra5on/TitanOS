package hwcheck

import (
	"bufio"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CPU is what /proc/cpuinfo says about the processor.
type CPU struct {
	// Vendor is intel, amd or the raw vendor_id of others.
	Vendor  string `json:"vendor"`
	Model   string `json:"model"`
	Threads int    `json:"threads"`
	// Cores counts physical cores (distinct package/core pairs); 0 if the
	// kernel does not say.
	Cores int `json:"cores"`
	// Level is the x86-64 microarchitecture level 1–4 (0: not x86-64).
	Level int `json:"level"`
	// Features are the instruction sets apps ask for, as far as present.
	Features []string `json:"features"`
	flags    map[string]bool
}

// Flags of the x86-64 levels as /proc/cpuinfo names them (psABI).
var levelFlags = [][]string{
	{"lm", "cmov", "cx8", "fpu", "fxsr", "mmx", "syscall", "sse", "sse2"},
	{"cx16", "lahf_lm", "popcnt", "pni", "sse4_1", "sse4_2", "ssse3"},
	{"avx", "avx2", "bmi1", "bmi2", "f16c", "fma", "abm", "movbe", "xsave"},
	{"avx512f", "avx512bw", "avx512cd", "avx512dq", "avx512vl"},
}

// shownFeatures are the flags the report and the app check talk about.
var shownFeatures = []string{"sse4_2", "aes", "avx", "avx2", "avx512f", "vmx", "svm"}

// ParseCPUInfo reads the text of /proc/cpuinfo.
func ParseCPUInfo(text string) CPU {
	var cpu CPU
	cores := map[string]bool{}
	physical, core := "", ""
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "processor":
			cpu.Threads++
			physical, core = "", ""
		case "vendor_id":
			cpu.Vendor = vendorName(value)
		case "model name":
			cpu.Model = strings.Join(strings.Fields(value), " ")
		case "physical id":
			physical = value
		case "core id":
			core = value
			cores[physical+"/"+core] = true
		case "flags":
			if cpu.flags == nil {
				cpu.flags = map[string]bool{}
				for _, flag := range strings.Fields(value) {
					cpu.flags[flag] = true
				}
			}
		}
	}
	cpu.Cores = len(cores)
	for level, flags := range levelFlags {
		if cpu.flags == nil || !slices.ContainsFunc(flags, func(flag string) bool { return !cpu.flags[flag] }) {
			if cpu.flags != nil {
				cpu.Level = level + 1
			}
			continue
		}
		break
	}
	cpu.Features = []string{}
	for _, flag := range shownFeatures {
		if cpu.flags[flag] {
			cpu.Features = append(cpu.Features, flag)
		}
	}
	return cpu
}

func vendorName(id string) string {
	switch id {
	case "GenuineIntel":
		return "intel"
	case "AuthenticAMD", "HygonGenuine":
		return "amd"
	}
	return id
}

// Has reports whether the CPU meets one requirement as NormalizeNeed
// spells it.
func (c CPU) Has(need string) bool {
	if level, ok := strings.CutPrefix(need, "x86-64-v"); ok {
		n, err := strconv.Atoi(level)
		return err == nil && c.Level >= n
	}
	return c.flags[need]
}

var flagName = regexp.MustCompile(`^[a-z0-9_]{2,24}$`)

// NormalizeNeed turns how templates write a requirement ("AVX2",
// "x86_64_v3", "sse4.2") into the names Has knows; "" if it is none.
func NormalizeNeed(need string) string {
	need = strings.ToLower(strings.TrimSpace(need))
	need = strings.ReplaceAll(need, ".", "_")
	switch need {
	case "avx512":
		return "avx512f"
	case "sse3":
		return "pni"
	}
	for _, prefix := range []string{"x86-64-v", "x86_64_v", "x86-64v", "x86_64-v"} {
		if level, ok := strings.CutPrefix(need, prefix); ok {
			if n, err := strconv.Atoi(level); err == nil && n >= 1 && n <= 4 {
				return "x86-64-v" + level
			}
			return ""
		}
	}
	if flagName.MatchString(need) {
		return need
	}
	return ""
}

// imageNeed is a known requirement of an image, from the maker's notes.
type imageNeed struct {
	repo string
	// fromMajor: tags from this major version on need it; 0 for all.
	fromMajor int
	needs     []string
}

// knownImages lists images whose builds need instructions older CPUs lack
// (Stand Oktober 2026, see docs/HARDWARE-COMPATIBILITY.md). A template can
// say more with x-titanos.cpu.
var knownImages = []imageNeed{
	{repo: "mongo", fromMajor: 5, needs: []string{"avx"}},
	{repo: "mongodb/mongodb-community-server", fromMajor: 5, needs: []string{"avx"}},
	{repo: "mongodb/mongodb-enterprise-server", fromMajor: 5, needs: []string{"avx"}},
	{repo: "bitnami/mongodb", fromMajor: 5, needs: []string{"avx"}},
	{repo: "ghcr.io/immich-app/immich-machine-learning", needs: []string{"avx"}},
	{repo: "altran1502/immich-machine-learning", needs: []string{"avx"}},
	{repo: "ghcr.io/blakeblackshear/frigate", needs: []string{"avx"}},
	{repo: "blakeblackshear/frigate", needs: []string{"avx"}},
}

// splitImage returns the repository without registry defaults and the tag.
func splitImage(image string) (string, string) {
	image = strings.ToLower(strings.TrimSpace(image))
	image, _, _ = strings.Cut(image, "@")
	repo, tag := image, ""
	if at := strings.LastIndex(image, ":"); at > strings.LastIndex(image, "/") {
		repo, tag = image[:at], image[at+1:]
	}
	for _, prefix := range []string{"docker.io/", "index.docker.io/", "registry-1.docker.io/"} {
		repo = strings.TrimPrefix(repo, prefix)
	}
	repo = strings.TrimPrefix(repo, "library/")
	return repo, tag
}

var leadingNumber = regexp.MustCompile(`^v?(\d+)`)

// ImageNeeds returns what the known images among images need.
func ImageNeeds(images []string) []string {
	var needs []string
	for _, image := range images {
		repo, tag := splitImage(image)
		for _, known := range knownImages {
			if known.repo != repo {
				continue
			}
			if known.fromMajor > 0 {
				// latest, stable and other words: the newest, which needs it.
				if match := leadingNumber.FindStringSubmatch(tag); match != nil {
					if major, _ := strconv.Atoi(match[1]); major < known.fromMajor {
						continue
					}
				}
			}
			for _, need := range known.needs {
				if !slices.Contains(needs, need) {
					needs = append(needs, need)
				}
			}
		}
	}
	sort.Strings(needs)
	return needs
}

// ComposeNeeds reads the CPU requirements of a compose file: the optional
// list x-titanos.cpu (top level or per service) and the known images.
func ComposeNeeds(compose string) []string {
	var file struct {
		Meta *struct {
			CPU []string `yaml:"cpu"`
		} `yaml:"x-titanos"`
		Services map[string]struct {
			Image string `yaml:"image"`
			Meta  *struct {
				CPU []string `yaml:"cpu"`
			} `yaml:"x-titanos"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(compose), &file) != nil {
		return []string{}
	}
	var listed, images []string
	if file.Meta != nil {
		listed = append(listed, file.Meta.CPU...)
	}
	for _, service := range file.Services {
		images = append(images, service.Image)
		if service.Meta != nil {
			listed = append(listed, service.Meta.CPU...)
		}
	}
	needs := ImageNeeds(images)
	for _, need := range listed {
		if need = NormalizeNeed(need); need != "" && !slices.Contains(needs, need) {
			needs = append(needs, need)
		}
	}
	sort.Strings(needs)
	if needs == nil {
		needs = []string{}
	}
	return needs
}

// Missing returns the requirements the CPU does not meet.
func (c CPU) Missing(needs []string) []string {
	missing := []string{}
	if c.flags == nil {
		return missing // unknown CPU: no warning rather than a false one
	}
	for _, need := range needs {
		if !c.Has(need) {
			missing = append(missing, need)
		}
	}
	return missing
}
