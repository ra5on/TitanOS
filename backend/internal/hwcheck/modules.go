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
// modules and built-in drivers, without blacklisted modules: a device that
// matches one has a driver in this kernel. Patterns with a fixed vendor are
// indexed by it.
type moduleAliases struct {
	byVendor map[string][]moduleAlias
	other    []moduleAlias
	modules  map[string]bool
}

// moduleAliases reads modules.alias of the kernel release and, where it is
// there as text, modules.builtin.alias (Debian ships it only as .bin; its
// network, storage, USB and graphics drivers are modules). nil if there are
// none (development machines, tests without modules).
func (c *Checker) moduleAliases(release string) *moduleAliases {
	if c.Modules == "" || release == "" {
		return nil
	}
	blocked := c.blacklisted()
	aliases := &moduleAliases{byVendor: map[string][]moduleAlias{}, modules: map[string]bool{}}
	found := false
	for _, name := range []string{"modules.alias", "modules.builtin.alias"} {
		file, err := os.Open(filepath.Join(c.Modules, release, name))
		if err != nil {
			continue
		}
		found = true
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
			module := normalizeModule(fields[2])
			if blocked[module] {
				continue
			}
			aliases.add(moduleAlias{fields[1], module})
		}
		file.Close()
	}
	if !found {
		return nil
	}
	return aliases
}

// vendorKey is the fixed bus and vendor part of a modalias or pattern
// ("pci:v00008086", "usb:v046D"), or "" if the pattern does not fix it.
func vendorKey(alias string) string {
	switch {
	case strings.HasPrefix(alias, "pci:v") && len(alias) >= 13:
		key := alias[:13]
		if strings.ContainsAny(key, "*?[") {
			return ""
		}
		return strings.ToUpper(key)
	case strings.HasPrefix(alias, "usb:v") && len(alias) >= 9:
		key := alias[:9]
		if strings.ContainsAny(key, "*?[") {
			return ""
		}
		return strings.ToUpper(key)
	}
	return ""
}

func (aliases *moduleAliases) add(entry moduleAlias) {
	aliases.modules[entry.module] = true
	if key := vendorKey(entry.pattern); key != "" {
		aliases.byVendor[key] = append(aliases.byVendor[key], entry)
		return
	}
	aliases.other = append(aliases.other, entry)
}

// module returns the first module whose pattern matches one of the
// newline-separated modaliases of a device, or "".
func (aliases *moduleAliases) module(modalias string) string {
	if aliases == nil {
		return ""
	}
	for _, alias := range strings.Split(modalias, "\n") {
		if alias == "" {
			continue
		}
		for _, list := range [][]moduleAlias{aliases.byVendor[vendorKey(alias)], aliases.other} {
			for _, entry := range list {
				// path.Match: modaliases hold no '/', and kmod's patterns
				// use only *, ? and [...].
				if matched, _ := path.Match(entry.pattern, alias); matched {
					return entry.module
				}
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
