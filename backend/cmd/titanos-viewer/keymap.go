package main

// evdevCodes maps KeyboardEvent.code to Linux evdev key codes (US
// physical layout). The X keycode is evdev + 8 (Xvfb uses the evdev
// XKB rules).
var evdevCodes = map[string]byte{
	"Escape": 1, "Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5, "Digit5": 6,
	"Digit6": 7, "Digit7": 8, "Digit8": 9, "Digit9": 10, "Digit0": 11,
	"Minus": 12, "Equal": 13, "Backspace": 14, "Tab": 15,
	"KeyQ": 16, "KeyW": 17, "KeyE": 18, "KeyR": 19, "KeyT": 20, "KeyY": 21,
	"KeyU": 22, "KeyI": 23, "KeyO": 24, "KeyP": 25,
	"BracketLeft": 26, "BracketRight": 27, "Enter": 28, "ControlLeft": 29,
	"KeyA": 30, "KeyS": 31, "KeyD": 32, "KeyF": 33, "KeyG": 34, "KeyH": 35,
	"KeyJ": 36, "KeyK": 37, "KeyL": 38, "Semicolon": 39, "Quote": 40,
	"Backquote": 41, "ShiftLeft": 42, "Backslash": 43,
	"KeyZ": 44, "KeyX": 45, "KeyC": 46, "KeyV": 47, "KeyB": 48, "KeyN": 49,
	"KeyM": 50, "Comma": 51, "Period": 52, "Slash": 53, "ShiftRight": 54,
	"NumpadMultiply": 55, "AltLeft": 56, "Space": 57, "CapsLock": 58,
	"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65,
	"F8": 66, "F9": 67, "F10": 68, "NumLock": 69, "ScrollLock": 70,
	"Numpad7": 71, "Numpad8": 72, "Numpad9": 73, "NumpadSubtract": 74,
	"Numpad4": 75, "Numpad5": 76, "Numpad6": 77, "NumpadAdd": 78,
	"Numpad1": 79, "Numpad2": 80, "Numpad3": 81, "Numpad0": 82,
	"NumpadDecimal": 83, "IntlBackslash": 86, "F11": 87, "F12": 88,
	"IntlRo": 89, "NumpadEnter": 96, "ControlRight": 97, "NumpadDivide": 98,
	"PrintScreen": 99, "AltRight": 100, "Home": 102, "ArrowUp": 103,
	"PageUp": 104, "ArrowLeft": 105, "ArrowRight": 106, "End": 107,
	"ArrowDown": 108, "PageDown": 109, "Insert": 110, "Delete": 111,
	"NumpadEqual": 117, "Pause": 119, "IntlYen": 124, "MetaLeft": 125,
	"MetaRight": 126, "ContextMenu": 127,
	"F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188,
	"F19": 189, "F20": 190, "F21": 191, "F22": 192, "F23": 193, "F24": 194,
}

// keyNameCodes maps KeyboardEvent.key names to a code for clients that
// send no code (e.g. some virtual keyboards).
var keyNameCodes = map[string]string{
	"Enter": "Enter", "Backspace": "Backspace", "Tab": "Tab", "Escape": "Escape",
	"Delete": "Delete", "Insert": "Insert", "Home": "Home", "End": "End",
	"PageUp": "PageUp", "PageDown": "PageDown", "ArrowUp": "ArrowUp",
	"ArrowDown": "ArrowDown", "ArrowLeft": "ArrowLeft", "ArrowRight": "ArrowRight",
	"Shift": "ShiftLeft", "Control": "ControlLeft", "Alt": "AltLeft", "Meta": "MetaLeft",
	"AltGraph": "AltRight", "CapsLock": "CapsLock", "ContextMenu": "ContextMenu",
	"F1": "F1", "F2": "F2", "F3": "F3", "F4": "F4", "F5": "F5", "F6": "F6",
	"F7": "F7", "F8": "F8", "F9": "F9", "F10": "F10", "F11": "F11", "F12": "F12",
	" ": "Space",
}

// usChars holds the characters of a code on the US layout without and
// with Shift. A printable key is sent with its physical keycode when it
// matches this table (so KeyboardEvent.code stays right in the remote
// browser); otherwise it is typed via a remapped spare keycode.
var usChars = map[string][2]rune{
	"Digit1": {'1', '!'}, "Digit2": {'2', '@'}, "Digit3": {'3', '#'}, "Digit4": {'4', '$'},
	"Digit5": {'5', '%'}, "Digit6": {'6', '^'}, "Digit7": {'7', '&'}, "Digit8": {'8', '*'},
	"Digit9": {'9', '('}, "Digit0": {'0', ')'}, "Minus": {'-', '_'}, "Equal": {'=', '+'},
	"BracketLeft": {'[', '{'}, "BracketRight": {']', '}'}, "Backslash": {'\\', '|'},
	"Semicolon": {';', ':'}, "Quote": {'\'', '"'}, "Backquote": {'`', '~'},
	"Comma": {',', '<'}, "Period": {'.', '>'}, "Slash": {'/', '?'}, "Space": {' ', ' '},
}

// usRune is where a character sits on the US layout.
type usRune struct {
	code  string
	shift bool
}

// usByRune is usChars the other way round: the key of a character.
var usByRune = map[rune]usRune{}

func init() {
	for c := 'a'; c <= 'z'; c++ {
		usChars["Key"+string(c-32)] = [2]rune{c, c - 32}
	}
	for code, chars := range usChars {
		usByRune[chars[0]] = usRune{code: code}
		if code != "Space" {
			usByRune[chars[1]] = usRune{code: code, shift: true}
		}
	}
}

// modifierCodes are the codes of modifier keys.
var modifierCodes = map[string]bool{
	"ShiftLeft": true, "ShiftRight": true, "ControlLeft": true, "ControlRight": true,
	"AltLeft": true, "AltRight": true, "MetaLeft": true, "MetaRight": true,
}

// xKeycode returns the X keycode for a KeyboardEvent.code (0 if unknown).
func xKeycode(code string) byte {
	if ev, ok := evdevCodes[code]; ok {
		return ev + 8
	}
	return 0
}

// keysymFor returns the X keysym that produces rune r.
func keysymFor(r rune) uint32 {
	switch {
	case r >= 0x20 && r <= 0x7e, r >= 0xa0 && r <= 0xff:
		return uint32(r)
	case r == '\n' || r == '\r':
		return 0xff0d
	case r == '\t':
		return 0xff09
	case r == '\b':
		return 0xff08
	}
	return 0x01000000 | uint32(r)
}

// singleRune returns the rune of a printable one-character key value.
func singleRune(key string) (rune, bool) {
	rs := []rune(key)
	if len(rs) != 1 {
		return 0, false
	}
	r := rs[0]
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
		return 0, false
	}
	return r, true
}
