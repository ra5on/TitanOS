package main

import (
	"bytes"
	"fmt"
)

// NAL unit types used here.
const (
	nalSlice = 1
	nalIDR   = 5
	nalSEI   = 6
	nalSPS   = 7
	nalPPS   = 8
	nalAUD   = 9
)

// accessUnit is one complete H.264 access unit in Annex B format.
type accessUnit struct {
	Data  []byte
	Key   bool   // contains an IDR slice
	Codec string // avc1.PPCCLL if the AU carries an SPS
}

// auSplitter splits an Annex B byte stream into access units. A new AU
// starts at an access unit delimiter (type 9), and – for encoders that
// do not emit delimiters – at SPS/PPS/SEI or a first slice
// (first_mb_in_slice == 0) after the current AU already holds a slice.
// An AU is complete when the start of the next one is seen, which adds
// one frame interval of latency.
type auSplitter struct {
	buf      []byte
	auStart  int  // start of the current AU in buf (-1: none yet)
	hasVCL   bool // current AU contains a slice
	scanFrom int  // next position to look for a start code
	maxAU    int
}

func newAUSplitter() *auSplitter {
	return &auSplitter{auStart: -1, maxAU: 8 << 20}
}

// findStartCode returns the index of the next 00 00 01 at or after i and
// the index of the start of the start code (one earlier for 00 00 00 01).
func findStartCode(b []byte, i int) (scStart, nalStart int) {
	j := bytes.Index(b[i:], []byte{0, 0, 1})
	if j < 0 {
		return -1, -1
	}
	j += i
	s := j
	if s > i && b[s-1] == 0 {
		s--
	}
	return s, j + 3
}

// Write feeds stream bytes and returns the access units completed by them.
func (s *auSplitter) Write(p []byte) []accessUnit {
	s.buf = append(s.buf, p...)
	var out []accessUnit
	for {
		sc, nal := findStartCode(s.buf, s.scanFrom)
		if sc < 0 || nal+1 >= len(s.buf) {
			// need the NAL header and the following byte
			if sc >= 0 {
				s.scanFrom = sc
			} else if len(s.buf) > 3 {
				s.scanFrom = len(s.buf) - 3
			}
			break
		}
		typ := s.buf[nal] & 0x1f
		startsAU := false
		switch typ {
		case nalAUD:
			startsAU = true
		case nalSPS, nalPPS, nalSEI:
			startsAU = s.hasVCL
		case nalSlice, nalIDR:
			// first_mb_in_slice is ue(v); value 0 is the single bit 1.
			startsAU = s.hasVCL && s.buf[nal+1]&0x80 != 0
		}
		if startsAU && s.auStart >= 0 {
			out = append(out, makeAU(s.buf[s.auStart:sc]))
			s.auStart = sc
			s.hasVCL = false
		}
		if s.auStart < 0 {
			s.auStart = sc
		}
		if typ == nalSlice || typ == nalIDR {
			s.hasVCL = true
		}
		s.scanFrom = nal
	}
	// compact: drop bytes before the current AU
	cut := s.auStart
	if cut < 0 {
		cut = s.scanFrom
	}
	if cut > 0 {
		n := copy(s.buf, s.buf[cut:])
		s.buf = s.buf[:n]
		s.scanFrom -= cut
		if s.auStart >= 0 {
			s.auStart -= cut
		}
	}
	if len(s.buf) > s.maxAU { // garbage or absurd frame: resync
		s.buf = s.buf[:0]
		s.auStart, s.scanFrom, s.hasVCL = -1, 0, false
	}
	return out
}

// makeAU copies the AU and inspects its NAL units.
func makeAU(b []byte) accessUnit {
	au := accessUnit{Data: append([]byte(nil), b...)}
	for _, n := range nalUnits(au.Data) {
		if len(n) == 0 {
			continue
		}
		switch n[0] & 0x1f {
		case nalIDR:
			au.Key = true
		case nalSPS:
			if len(n) >= 4 {
				au.Codec = fmt.Sprintf("avc1.%02X%02X%02X", n[1], n[2], n[3])
			}
		}
	}
	return au
}

// nalUnits returns the NAL units (without start codes) of an Annex B buffer.
func nalUnits(b []byte) [][]byte {
	var out [][]byte
	_, nal := findStartCode(b, 0)
	for nal >= 0 && nal <= len(b) {
		next, nn := findStartCode(b, nal)
		if next < 0 {
			out = append(out, b[nal:])
			break
		}
		out = append(out, b[nal:next])
		nal = nn
	}
	return out
}

// nalTypes lists the NAL unit types of an AU (for tests and checks).
func nalTypes(b []byte) []int {
	var t []int
	for _, n := range nalUnits(b) {
		if len(n) > 0 {
			t = append(t, int(n[0]&0x1f))
		}
	}
	return t
}
