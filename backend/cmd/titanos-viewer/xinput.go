package main

import (
	"context"
	"errors"
	"io"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// spareSlot is a keycode without keysyms that is remapped to type
// characters the US layout cannot produce (like xdotool does).
type spareSlot struct {
	kc   xproto.Keycode
	r    rune
	used time.Time
	// fixed slots get their character once, before Chromium starts, and
	// keep it (fixedRunes).
	fixed bool
}

// fixedRunes are characters no US key carries that get a keycode of their
// own at the start. Chromium reads the keyboard mapping when it starts: a
// keycode remapped later still types its character, but the keydown event
// names no key (key "", keyCode 0), and programs that collect the keys
// themselves (Selkies in Bambu Studio, remote desktops, games) drop it.
// A key mapped from the start is an ordinary key: it can be held, repeats
// and has a name. German first (PLAN 90-3); the other characters keep
// the remapped keycodes.
var fixedRunes = []rune{'ä', 'ö', 'ü', 'Ä', 'Ö', 'Ü', 'ß', '€', '§', '°', 'µ'}

// dynamicSpare is how many spare keycodes stay free for remapping.
const dynamicSpare = 4

// xctl injects input via XTest and manages windows and the cursor.
type xctl struct {
	conn *xgb.Conn
	root xproto.Window

	mu      sync.Mutex
	spare   []spareSlot
	pressed map[string]xproto.Keycode // code → pressed keycode
	buttons int
	accX    float64
	accY    float64
	w, h    int
}

func connectX() (*xctl, error) {
	xgb.Logger = log.New(io.Discard, "", 0)
	var conn *xgb.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = xgb.NewConnDisplay(display)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, err
	}
	setup := xproto.Setup(conn)
	x := &xctl{conn: conn, root: setup.DefaultScreen(conn).Root, pressed: map[string]xproto.Keycode{}}
	if err := x.findSpare(setup.MinKeycode, setup.MaxKeycode); err != nil {
		conn.Close()
		return nil, err
	}
	return x, nil
}

// findSpare collects keycodes without any keysym, leaving out the ones
// used by the code table.
func (x *xctl) findSpare(minKC, maxKC xproto.Keycode) error {
	count := int(maxKC) - int(minKC) + 1
	rep, err := xproto.GetKeyboardMapping(x.conn, minKC, byte(count)).Reply()
	if err != nil {
		return err
	}
	used := map[xproto.Keycode]bool{}
	for _, ev := range evdevCodes {
		used[xproto.Keycode(ev+8)] = true
	}
	per := int(rep.KeysymsPerKeycode)
	for i := 0; i < count; i++ {
		kc := xproto.Keycode(int(minKC) + i)
		empty := true
		for j := 0; j < per; j++ {
			if rep.Keysyms[i*per+j] != 0 {
				empty = false
				break
			}
		}
		if empty && !used[kc] {
			x.spare = append(x.spare, spareSlot{kc: kc})
		}
	}
	if len(x.spare) < 4 {
		return errors.New("no spare keycodes for typing")
	}
	// Sanity check: the code table assumes evdev keycodes (KeyA = 38).
	if a := 38 - int(minKC); a >= 0 && a < count && rep.Keysyms[a*per] != 'a' {
		log.Printf("warning: X keymap is not evdev based (keycode 38 = %#x)", rep.Keysyms[a*per])
	}
	fixed := 0
	for i := 0; i < len(fixedRunes) && i < len(x.spare)-dynamicSpare; i++ {
		ks := xproto.Keysym(keysymFor(fixedRunes[i]))
		xproto.ChangeKeyboardMapping(x.conn, 1, x.spare[i].kc, 2, []xproto.Keysym{ks, ks})
		x.spare[i].r, x.spare[i].fixed = fixedRunes[i], true
		fixed++
	}
	x.sync()
	log.Printf("input: %d spare keycodes, %d with a fixed character", len(x.spare), fixed)
	return nil
}

// fixedKeycode is the keycode that carries r from the start.
func (x *xctl) fixedKeycode(r rune) (xproto.Keycode, bool) {
	for _, s := range x.spare {
		if s.fixed && s.r == r {
			return s.kc, true
		}
	}
	return 0, false
}

func (x *xctl) sync() {
	_, _ = xproto.GetInputFocus(x.conn).Reply()
}

func (x *xctl) fake(typ byte, detail byte, px, py int16) {
	xtest.FakeInput(x.conn, typ, detail, 0, x.root, px, py, 0)
}

func (x *xctl) setScreen(w, h int) {
	x.mu.Lock()
	x.w, x.h = w, h
	x.mu.Unlock()
}

// buttonMap maps MouseEvent.buttons bits to X buttons.
var buttonMap = [5]byte{1, 3, 2, 8, 9}

// mouse moves the pointer (absolute, screen pixels) and applies the
// button changes against the last known state.
func (x *xctl) mouse(px, py, buttons int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	px = clamp(px, 0, x.w-1)
	py = clamp(py, 0, x.h-1)
	x.fake(xproto.MotionNotify, 0, int16(px), int16(py))
	x.applyButtons(buttons)
}

func (x *xctl) applyButtons(buttons int) {
	for bit, xb := range buttonMap {
		was := x.buttons&(1<<bit) != 0
		now := buttons&(1<<bit) != 0
		switch {
		case now && !was:
			x.fake(xproto.ButtonPress, xb, 0, 0)
		case !now && was:
			x.fake(xproto.ButtonRelease, xb, 0, 0)
		}
	}
	x.buttons = buttons & 31
}

// wheelSteps converts accumulated pixel deltas into wheel clicks
// (100 px per click, at most 20 per message).
func wheelSteps(acc *float64, d float64) int {
	*acc += d
	n := int(*acc / 100)
	*acc -= float64(n) * 100
	if n > 20 {
		n = 20
	}
	if n < -20 {
		n = -20
	}
	return n
}

func (x *xctl) wheel(dx, dy float64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	click := func(b byte, n int) {
		for i := 0; i < n; i++ {
			x.fake(xproto.ButtonPress, b, 0, 0)
			x.fake(xproto.ButtonRelease, b, 0, 0)
		}
	}
	if n := wheelSteps(&x.accY, dy); n > 0 {
		click(5, n)
	} else if n < 0 {
		click(4, -n)
	}
	if n := wheelSteps(&x.accX, dx); n > 0 {
		click(7, n)
	} else if n < 0 {
		click(6, -n)
	}
}

// keyAction is what key() decided to do; split out for tests.
type keyAction int

const (
	keyIgnore   keyAction = iota
	keyPhysical           // press/release the keycode of the returned key
	keyTap                // tap the US key of the rune with the Shift it needs (on down)
	keyType               // type the rune via a spare keycode (on down)
)

// decideKey picks how to deliver a key message. shiftHeld is the X-side
// shift state. code is the key of the user's keyboard (what is held and
// released later), press the key of the US layout that goes to X.
//
// A character goes over as the US key that carries it wherever there is
// one, also when it sits elsewhere on the user's keyboard (German z is the
// key KeyY there and the key KeyZ here): the remote program then gets an
// ordinary key with a name that can be held. Only characters no US key
// carries are typed via a spare keycode. With Ctrl, Alt or Meta the
// letter on the key counts too, so Ctrl+Z stays Ctrl+Z on a German
// keyboard instead of becoming Ctrl+Y.
func decideKey(m proto.ClientMsg, shiftHeld bool) (act keyAction, code, press string, r rune) {
	code = m.Code
	if code == "" || xKeycode(code) == 0 {
		if c, ok := keyNameCodes[m.Key]; ok {
			code = c
		}
	}
	r, printable := singleRune(m.Key)
	// AltGr arrives as Ctrl+Alt on Windows: treat as a typed character.
	plain := !m.Mods.Ctrl && !m.Mods.Alt && !m.Mods.Meta
	altGr := m.Mods.Ctrl && m.Mods.Alt && !m.Mods.Meta
	if printable && (plain || altGr) && !modifierCodes[code] {
		if chars, ok := usChars[code]; ok && plain {
			want := chars[0]
			if shiftHeld {
				want = chars[1]
			}
			if want == r {
				return keyPhysical, code, code, r
			}
		}
		if us, ok := usByRune[r]; ok {
			if plain && us.shift == shiftHeld {
				return keyPhysical, code, us.code, r
			}
			return keyTap, code, us.code, r
		}
		return keyType, code, "", r
	}
	if printable && !altGr && !modifierCodes[code] {
		if l := unicode.ToLower(r); l >= 'a' && l <= 'z' {
			return keyPhysical, code, "Key" + string(unicode.ToUpper(l)), 0
		}
	}
	if xKeycode(code) == 0 {
		return keyIgnore, code, "", 0
	}
	return keyPhysical, code, code, 0
}

func (x *xctl) shiftHeldLocked() bool {
	_, l := x.pressed["ShiftLeft"]
	_, r := x.pressed["ShiftRight"]
	return l || r
}

func (x *xctl) key(m proto.ClientMsg) {
	x.mu.Lock()
	defer x.mu.Unlock()
	act, code, press, r := decideKey(m, x.shiftHeldLocked())
	if !m.Down {
		// Whatever went down for this key of the user's keyboard goes up,
		// also when the character changed meanwhile (Shift let go first).
		if kc, ok := x.pressed[code]; ok {
			x.fake(xproto.KeyRelease, byte(kc), 0, 0)
			delete(x.pressed, code)
		} else if act == keyPhysical {
			if kc := xKeycode(press); kc != 0 {
				x.fake(xproto.KeyRelease, kc, 0, 0)
			}
		}
		return
	}
	hold := func(kc xproto.Keycode) {
		if old, ok := x.pressed[code]; ok && old != kc {
			x.fake(xproto.KeyRelease, byte(old), 0, 0)
		}
		x.fake(xproto.KeyPress, byte(kc), 0, 0)
		x.pressed[code] = kc
	}
	switch act {
	case keyType:
		plain := !m.Mods.Ctrl && !m.Mods.Alt && !m.Mods.Meta
		if kc, ok := x.fixedKeycode(r); ok && plain {
			hold(kc)
			return
		}
		x.typeRuneLocked(r)
	case keyTap:
		x.tapShiftedLocked(xproto.Keycode(xKeycode(press)), usByRune[r].shift)
	case keyPhysical:
		if kc := xKeycode(press); kc != 0 {
			hold(xproto.Keycode(kc))
		}
	}
}

// tapShiftedLocked taps a key with Shift as the character needs it,
// whatever is held: German Shift+7 is "/", a key without Shift here;
// German "#" has a key of its own and is Shift+3 here. Held Ctrl, Alt and
// Meta are let go meanwhile (AltGr characters must not become shortcuts).
func (x *xctl) tapShiftedLocked(kc xproto.Keycode, shift bool) {
	var held []xproto.Keycode
	for code, pkc := range x.pressed {
		if !modifierCodes[code] {
			continue
		}
		if isShift := strings.HasPrefix(code, "Shift"); !isShift || !shift {
			held = append(held, pkc)
			x.fake(xproto.KeyRelease, byte(pkc), 0, 0)
		}
	}
	need := shift && !x.shiftHeldLocked()
	if need {
		x.fake(xproto.KeyPress, xKeycode("ShiftLeft"), 0, 0)
	}
	x.tapLocked(kc)
	if need {
		x.fake(xproto.KeyRelease, xKeycode("ShiftLeft"), 0, 0)
	}
	for _, pkc := range held {
		x.fake(xproto.KeyPress, byte(pkc), 0, 0)
	}
}

// text types a string character by character. Characters of the US
// layout are sent as physical keys (with Shift where needed); the others
// via remapped spare keycodes. The lock is taken per character so mouse
// and keys keep working during a long text.
func (x *xctl) text(ctx context.Context, s string) {
	for _, r := range s {
		if ctx.Err() != nil {
			return
		}
		x.mu.Lock()
		switch {
		case r == '\r':
		case r == '\n':
			x.tapLocked(xproto.Keycode(xKeycode("Enter")))
		case r == '\t':
			x.tapLocked(xproto.Keycode(xKeycode("Tab")))
		case r < 0x20 || r == 0x7f:
		default:
			if kc, shift, ok := usKey(r); ok && !(x.shiftHeldLocked() && !shift) {
				if shift && !x.shiftHeldLocked() {
					x.fake(xproto.KeyPress, xKeycode("ShiftLeft"), 0, 0)
					x.tapLocked(xproto.Keycode(kc))
					x.fake(xproto.KeyRelease, xKeycode("ShiftLeft"), 0, 0)
				} else {
					x.tapLocked(xproto.Keycode(kc))
				}
			} else {
				x.typeRuneLocked(r)
			}
		}
		x.mu.Unlock()
	}
	x.sync()
}

// usKey finds the physical key (and whether Shift is needed) for a rune
// on the US layout.
func usKey(r rune) (kc byte, shift bool, ok bool) {
	us, ok := usByRune[r]
	if !ok {
		return 0, false, false
	}
	return xKeycode(us.code), us.shift, true
}

func (x *xctl) tapLocked(kc xproto.Keycode) {
	x.fake(xproto.KeyPress, byte(kc), 0, 0)
	x.fake(xproto.KeyRelease, byte(kc), 0, 0)
}

// typeRuneLocked maps r to a spare keycode (reusing a slot that already
// holds r) and taps it. Held Ctrl/Alt/Meta are released meanwhile so
// AltGr characters do not become shortcuts. A slot is reused for another
// character only after 150 ms, so the browser has processed the earlier
// key with the earlier mapping.
func (x *xctl) typeRuneLocked(r rune) {
	idx := -1
	oldest := -1
	for i, s := range x.spare {
		if s.r == r {
			idx = i
			break
		}
		if !s.fixed && (oldest < 0 || s.used.Before(x.spare[oldest].used)) {
			oldest = i
		}
	}
	if idx < 0 {
		idx = oldest
		if wait := 150*time.Millisecond - time.Since(x.spare[idx].used); wait > 0 {
			time.Sleep(wait)
		}
		ks := xproto.Keysym(keysymFor(r))
		xproto.ChangeKeyboardMapping(x.conn, 1, x.spare[idx].kc, 2, []xproto.Keysym{ks, ks})
		x.spare[idx].r = r
		x.sync()
		time.Sleep(5 * time.Millisecond)
	}
	var held []xproto.Keycode
	for code, kc := range x.pressed {
		if modifierCodes[code] && !strings.HasPrefix(code, "Shift") {
			held = append(held, kc)
			x.fake(xproto.KeyRelease, byte(kc), 0, 0)
		}
	}
	x.tapLocked(x.spare[idx].kc)
	for _, kc := range held {
		x.fake(xproto.KeyPress, byte(kc), 0, 0)
	}
	x.spare[idx].used = time.Now()
}

// releaseAll releases all keys and buttons (client gone).
func (x *xctl) releaseAll() {
	x.mu.Lock()
	defer x.mu.Unlock()
	for code, kc := range x.pressed {
		x.fake(xproto.KeyRelease, byte(kc), 0, 0)
		delete(x.pressed, code)
	}
	x.applyButtons(0)
	x.sync()
}

// closeWindows sends WM_DELETE_WINDOW to every top-level window that
// supports it, like a window manager's close button.
func (x *xctl) closeWindows() {
	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(x.conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return r.Atom
	}
	protocols, del := atom("WM_PROTOCOLS"), atom("WM_DELETE_WINDOW")
	if protocols == 0 || del == 0 {
		return
	}
	tree, err := xproto.QueryTree(x.conn, x.root).Reply()
	if err != nil {
		return
	}
	n := 0
	for _, win := range tree.Children {
		prop, err := xproto.GetProperty(x.conn, false, win, protocols, xproto.AtomAtom, 0, 32).Reply()
		if err != nil || prop.Format != 32 {
			continue
		}
		supported := false
		for i := 0; i+4 <= len(prop.Value); i += 4 {
			if xproto.Atom(xgb.Get32(prop.Value[i:])) == del {
				supported = true
			}
		}
		if !supported {
			continue
		}
		ev := xproto.ClientMessageEvent{Format: 32, Window: win, Type: protocols,
			Data: xproto.ClientMessageDataUnionData32New([]uint32{uint32(del), 0, 0, 0, 0})}
		xproto.SendEvent(x.conn, false, win, xproto.EventMaskNoEvent, string(ev.Bytes()))
		n++
	}
	x.sync()
	log.Printf("asked %d browser windows to close", n)
}

// fitWindows keeps every normal top-level window at 0,0 with the screen
// size (no window manager runs; a page going fullscreen would otherwise
// grow to the whole 3840x2160 X screen). Override-redirect windows
// (menus, pop-ups) are left alone.
func (x *xctl) fitWindows() {
	x.mu.Lock()
	w, h := x.w, x.h
	x.mu.Unlock()
	if w == 0 {
		return
	}
	tree, err := xproto.QueryTree(x.conn, x.root).Reply()
	if err != nil {
		return
	}
	for _, win := range tree.Children {
		attr, err := xproto.GetWindowAttributes(x.conn, win).Reply()
		if err != nil || attr.MapState != xproto.MapStateViewable || attr.OverrideRedirect {
			continue
		}
		g, err := xproto.GetGeometry(x.conn, xproto.Drawable(win)).Reply()
		if err != nil || g.Width <= 50 || g.Height <= 50 {
			continue
		}
		if g.X != 0 || g.Y != 0 || int(g.Width) != w || int(g.Height) != h {
			xproto.ConfigureWindow(x.conn, win,
				xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
				[]uint32{0, 0, uint32(w), uint32(h)})
		}
	}
	x.sync()
}

// cursorCSS maps X cursor names (Xcursor/XFixes) to CSS cursor names.
func cursorCSS(name string) string {
	switch name {
	case "", "left_ptr", "default", "arrow", "top_left_arrow":
		return "default"
	case "xterm", "text", "ibeam":
		return "text"
	case "hand2", "hand1", "pointer", "pointing_hand":
		return "pointer"
	case "watch", "wait":
		return "wait"
	case "left_ptr_watch", "progress":
		return "progress"
	case "crosshair", "cross":
		return "crosshair"
	case "fleur", "move", "all-scroll", "size_all":
		return "move"
	case "grab", "openhand":
		return "grab"
	case "grabbing", "closedhand":
		return "grabbing"
	case "not-allowed", "crossed_circle", "forbidden":
		return "not-allowed"
	case "sb_h_double_arrow", "ew-resize", "col-resize", "size_hor", "h_double_arrow":
		return "ew-resize"
	case "sb_v_double_arrow", "ns-resize", "row-resize", "size_ver", "v_double_arrow":
		return "ns-resize"
	case "help", "question_arrow":
		return "help"
	case "none":
		return "none"
	}
	if strings.HasSuffix(name, "-resize") || name == "copy" || name == "alias" ||
		name == "cell" || name == "context-menu" || name == "zoom-in" || name == "zoom-out" {
		return name
	}
	return "default"
}

// watchCursor reports cursor changes via XFixes (optional).
func (x *xctl) watchCursor(cb func(string)) {
	if err := xfixes.Init(x.conn); err != nil {
		log.Printf("cursor: no XFIXES: %v", err)
		return
	}
	if _, err := xfixes.QueryVersion(x.conn, 5, 0).Reply(); err != nil {
		log.Printf("cursor: %v", err)
		return
	}
	xfixes.SelectCursorInput(x.conn, x.root, xfixes.CursorNotifyMaskDisplayCursor)
	go func() {
		last := ""
		for {
			ev, err := x.conn.WaitForEvent()
			if ev == nil && err == nil {
				return // connection closed
			}
			if _, ok := ev.(xfixes.CursorNotifyEvent); !ok {
				continue
			}
			rep, rerr := xfixes.GetCursorImageAndName(x.conn).Reply()
			if rerr != nil {
				continue
			}
			shape := cursorCSS(rep.Name)
			if shape != last {
				last = shape
				cb(shape)
			}
		}
	}()
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return int(math.Max(float64(lo), math.Min(float64(hi), float64(v))))
}
