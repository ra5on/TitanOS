package vm

import (
	"unicode"
	"unicode/utf8"
)

func validDisplayName(name string) bool {
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 80 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// Label is for people; Name remains the stable domain, path and API key.
func (vm VM) Label() string {
	if vm.DisplayName != "" {
		return vm.DisplayName
	}
	return vm.Name
}
