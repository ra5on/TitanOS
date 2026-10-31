package update

// Kernel choice "Stabil / Neu" (docs/KERNEL.md). A release carries the
// system with Debian's kernel (the manifest's own files, which every version
// of TitanOS installs) and, optionally, the same system with the newer kernel
// of trixie-backports under variants.new. The choice in the update settings
// decides which one an update writes into the other slot; switching at the
// same version is a system update of its own. The old slot stays the
// fallback, so a kernel that does not start goes back by itself.

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"strings"
)

const (
	// VariantStable is Debian's kernel of the release ("" in settings).
	VariantStable = "stable"
	// VariantNew is the newer kernel from trixie-backports.
	VariantNew = "new"
	// VariantFile names the kernel variant of a system image.
	VariantFile = "/usr/share/titanos/kernel-variant"
)

// Variant is a system of a release with another kernel. It has the same
// version, cmdline and app as the release; plugins for its kernel come from
// the plugin repository (kernel sets by KernelPackage).
type Variant struct {
	Kernel        string `json:"kernel"`
	KernelPackage string `json:"kernelPackage,omitempty"`
	Files         []File `json:"files"`
}

// ReadVariant returns the kernel variant of the running image.
func ReadVariant(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return VariantStable
	}
	return normalVariant(strings.TrimSpace(string(data)))
}

func normalVariant(v string) string {
	if v == VariantNew {
		return VariantNew
	}
	return VariantStable
}

func validVariant(v string) bool {
	return v == "" || v == VariantStable || v == VariantNew
}

// forVariant returns the manifest as it installs the given kernel variant:
// the stable one is the manifest itself. ok is false if the release does
// not carry that variant.
func (man Manifest) forVariant(variant string) (Manifest, bool) {
	if normalVariant(variant) == VariantStable {
		return man, true
	}
	v, ok := man.Variants[VariantNew]
	if !ok {
		return Manifest{}, false
	}
	out := man
	out.Kernel, out.KernelPackage, out.Files = v.Kernel, v.KernelPackage, v.Files
	// Plugins for this kernel come from the plugin repository only.
	out.Plugins = nil
	out.Variants = nil
	return out, true
}

// wantedVariant is the kernel variant the settings ask for.
func (m *Manager) wantedVariant() string {
	return normalVariant(m.settings.Kernel)
}

// kernelRelease reads the kernel release ("6.12.48+deb13-amd64") from the
// header of a bzImage. Only the first page or so is read.
func kernelRelease(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 0x210)
	if _, err := io.ReadFull(f, head); err != nil || string(head[0x202:0x206]) != "HdrS" {
		return ""
	}
	offset := int64(binary.LittleEndian.Uint16(head[0x20E:0x210]))
	if offset == 0 {
		return ""
	}
	text := make([]byte, 128)
	n, _ := f.ReadAt(text, offset+0x200)
	text = text[:n]
	if i := bytes.IndexAny(text, " \x00"); i >= 0 {
		text = text[:i]
	}
	release := string(text)
	for _, r := range release {
		if r < 0x21 || r > 0x7e {
			return ""
		}
	}
	return release
}

// kernelVariant tells from a kernel release which variant it belongs to:
// Debian's backports kernels carry "bpo" in their release.
func kernelVariant(release string) string {
	if release == "" {
		return ""
	}
	if strings.Contains(release, "bpo") {
		return VariantNew
	}
	return VariantStable
}
