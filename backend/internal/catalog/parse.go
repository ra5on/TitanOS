package catalog

import (
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// localized is a text that a template gives either once or per language.
type localized map[string]string

func (l *localized) UnmarshalYAML(node *yaml.Node) error {
	*l = localized{}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!null" {
			(*l)[""] = node.Value
		}
	case yaml.MappingNode:
		// One entry of the wrong kind does not spoil the others.
		for i := 0; i+1 < len(node.Content); i += 2 {
			if key, value := node.Content[i], node.Content[i+1]; value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
				(*l)[key.Value] = value.Value
			}
		}
	}
	return nil
}

// languages reduces the locales of a CasaOS template ("de_DE", "en_US",
// "en_GB") to the languages TitanOS speaks.
func (l localized) languages() map[string]string {
	texts := map[string]string{}
	for _, locale := range []string{"en_gb", "en_us", "de_de"} {
		for key, text := range l {
			if strings.ToLower(key) == locale && strings.TrimSpace(text) != "" {
				texts[locale[:2]] = text
			}
		}
	}
	if text, ok := l[""]; ok && len(texts) == 0 {
		texts[""] = text
	}
	return texts
}

// template is what the store needs to know of a compose file. The web UI
// reads the rest when an app is installed.
type template struct {
	Services yaml.Node `yaml:"services"`
	Meta     *struct {
		Name        string    `yaml:"name"`
		Icon        string    `yaml:"icon"`
		Category    string    `yaml:"category"`
		Description localized `yaml:"description"`
	} `yaml:"x-titanos"`
	// CasaOS is the metadata of a template from a CasaOS store. It is read
	// field by field: stores are not strict about it.
	CasaOS map[string]yaml.Node `yaml:"x-casaos"`
}

var (
	appIDRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	notInID     = regexp.MustCompile(`[^a-z0-9_-]+`)
	composeName = regexp.MustCompile(`^(docker-)?compose\.ya?ml$`)
)

// casaCategories maps the categories of CasaOS stores to those of TitanOS.
var casaCategories = map[string]string{
	"media": "media", "productivity": "productivity", "networking": "network", "network": "network",
	"developer": "development", "home": "home", "ai": "ai", "finance": "finance", "social": "social",
	"backup": "backup", "security": "security", "others": "other", "other": "other",
}

// appID derives the name of an app from where its compose file is:
// "jellyfin/compose.yaml" and "jellyfin.yaml" are both "jellyfin".
func appID(file string) string {
	base := path.Base(file)
	name := strings.TrimSuffix(base, path.Ext(base))
	if composeName.MatchString(strings.ToLower(base)) {
		name = path.Base(path.Dir(file))
	}
	name = strings.Trim(notInID.ReplaceAllString(strings.ToLower(name), "-"), "-_")
	if len(name) > 32 {
		name = strings.TrimRight(name[:32], "-_")
	}
	return name
}

func clip(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > n {
		return string(runes[:n-1]) + "…"
	}
	return text
}

// parse turns the YAML files of a source into apps. Files that are not
// compose files are skipped.
func parse(source string, files map[string]string) ([]App, string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	apps := []App{}
	seen := map[string]bool{}
	formats := map[string]int{}
	for _, name := range names {
		var tpl template
		if yaml.Unmarshal([]byte(files[name]), &tpl) != nil || tpl.Services.Kind != yaml.MappingNode {
			continue
		}
		app := App{ID: appID(name), Source: source, Description: map[string]string{}, compose: files[name]}
		usable := true
		for i := 1; i < len(tpl.Services.Content); i += 2 {
			var service struct {
				Image string `yaml:"image"`
			}
			// A service that has to be built needs files the store does not have.
			if tpl.Services.Content[i].Decode(&service) != nil || service.Image == "" {
				usable = false
				break
			}
			if app.Services == 0 {
				app.Image = service.Image
			}
			app.Services++
		}
		if !usable || app.Services == 0 || !appIDRE.MatchString(app.ID) || seen[app.ID] {
			continue
		}
		app.Name = app.ID
		format := "compose"
		switch {
		case tpl.Meta != nil:
			format = "titanos"
			app.Icon = safeIcon(tpl.Meta.Icon)
			if name := clip(tpl.Meta.Name, 60); name != "" {
				app.Name = name
			}
			app.Category = clip(strings.ToLower(tpl.Meta.Category), 30)
			for language, text := range tpl.Meta.Description {
				app.Description[language] = clip(text, 300)
			}
		case tpl.CasaOS != nil:
			format = "casaos"
			if !readCasaOS(&app, tpl.CasaOS) {
				continue
			}
		}
		seen[app.ID] = true
		formats[format]++
		apps = append(apps, app)
	}
	if len(apps) == 0 {
		return nil, "", ErrEmpty
	}
	// A source is called after what most of its templates are; plain
	// compose files only count if there is nothing else.
	format := "compose"
	for _, candidate := range []string{"casaos", "titanos"} {
		if formats[candidate] > 0 && (format == "compose" || formats[candidate] >= formats[format]) {
			format = candidate
		}
	}
	return apps, format, nil
}

// readCasaOS fills in what a CasaOS template says about itself. It
// returns false for an app that does not run on this kind of machine.
func readCasaOS(app *App, meta map[string]yaml.Node) bool {
	field := func(key string, into any) {
		if node, ok := meta[key]; ok {
			node.Decode(into)
		}
	}
	var architectures []string
	field("architectures", &architectures)
	if len(architectures) > 0 && !slices.Contains(architectures, "amd64") {
		return false
	}
	var title, tagline, description localized
	var category, icon string
	field("icon", &icon)
	app.Icon = safeIcon(icon)
	field("title", &title)
	field("tagline", &tagline)
	field("description", &description)
	field("category", &category)

	names := title.languages()
	for _, language := range []string{"en", "de", ""} {
		if name := clip(names[language], 60); name != "" {
			app.Name = name
			break
		}
	}
	// The tagline is the one-liner; descriptions run to several paragraphs.
	texts := tagline.languages()
	if len(texts) == 0 {
		texts = description.languages()
	}
	for language, text := range texts {
		app.Description[language] = clip(text, 300)
	}
	category = strings.ToLower(strings.TrimSpace(category))
	if known, ok := casaCategories[category]; ok {
		category = known
	}
	app.Category = clip(category, 30)
	return true
}

// MissingDevices returns the devices a compose file hands to its
// containers that this machine does not have. A container with such a
// device does not start; templates list devices of other hardware freely.
func MissingDevices(compose string) []string {
	var file struct {
		Services map[string]struct {
			Devices []yaml.Node `yaml:"devices"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(compose), &file) != nil {
		return []string{}
	}
	missing := []string{}
	for _, service := range file.Services {
		for _, device := range service.Devices {
			if device.Kind != yaml.ScalarNode {
				continue
			}
			host, _, _ := strings.Cut(device.Value, ":")
			if !strings.HasPrefix(host, "/dev/") || slices.Contains(missing, host) {
				continue
			}
			if _, err := os.Stat(host); err != nil {
				missing = append(missing, host)
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// Images are browser-fetched URLs, never HTML or server-side requests.
func safeIcon(value string) string {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(value) > 2048 {
		return ""
	}
	return value
}
