package hwcheck

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// moduleAlias is one "alias <pattern> <module>" line of modules.alias.
type moduleAlias struct{ pattern, module string }

// moduleAliases are the PCI and USB patterns of the running kernel's
// modules, without blacklisted modules: a device that matches one has a
// driver in this kernel.
type moduleAliases []moduleAlias

// moduleAliases reads modules.alias of the kernel release; nil if it is
// missing (development machines, tests without modules).
func (c *Checker) moduleAliases(release string) moduleAliases {
	if c.Modules == "" || release == "" {
		return nil
	}
	file, err := os.Open(filepath.Join(c.Modules, release, "modules.alias"))
	if err != nil {
		return nil
	}
	defer file.Close()
	blocked := c.blacklisted()
	var aliases moduleAliases
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[0] != "alias" {
			continue
		}
		if !strings.HasPrefix(fields[1], "pci:") && !strings.HasPrefix(fields[1], "usb:") {
			continue
		}
		if module := normalizeModule(fields[2]); !blocked[module] {
			aliases = append(aliases, moduleAlias{fields[1], module})
		}
	}
	return aliases
}

// module returns the first module whose pattern matches one of the
// newline-separated modaliases of a device, or "".
func (aliases moduleAliases) module(modalias string) string {
	for _, alias := range strings.Split(modalias, "\n") {
		if alias == "" {
			continue
		}
		for _, entry := range aliases {
			// path.Match: modaliases hold no '/', and kmod's patterns
			// use only *, ? and [...].
			if matched, _ := path.Match(entry.pattern, alias); matched {
				return entry.module
			}
		}
	}
	return ""
}

// blacklisted reads "blacklist <module>" from the modprobe.d directories:
// such a module is in the image on purpose without being loaded for a
// device (nouveau, for example).
func (c *Checker) blacklisted() map[string]bool {
	blocked := map[string]bool{}
	for _, dir := range c.Modprobe {
		files, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
		for _, name := range files {
			data, err := os.ReadFile(name)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "blacklist" {
					blocked[normalizeModule(fields[1])] = true
				}
			}
		}
	}
	return blocked
}

func normalizeModule(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// driverMessages returns the last kernel lines the module wrote about the
// device ("vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)"), at most
// three, each at most 300 bytes.
func driverMessages(log, module, address string) []string {
	var lines []string
	prefixes := []string{module + " " + address + ":", strings.ReplaceAll(module, "_", "-") + " " + address + ":"}
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				if len(line) > 300 {
					line = line[:300]
				}
				lines = append(lines, line)
				break
			}
		}
	}
	// The driver may be registered under another name than its module.
	if len(lines) == 0 {
		for _, line := range strings.Split(log, "\n") {
			line = strings.TrimSpace(line)
			word, rest, _ := strings.Cut(line, " ")
			if word == "pci" || word == "pcieport" || !strings.HasPrefix(rest, address+":") {
				continue
			}
			if len(line) > 300 {
				line = line[:300]
			}
			lines = append(lines, line)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return lines
}
