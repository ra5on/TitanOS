package dnsprovider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"
)

// rfc2136 sends DNS UPDATE messages (RFC 2136) signed with a TSIG key
// (RFC 8945) to the primary name server of the zone, over TCP. Works with
// BIND, Knot, PowerDNS and others that allow updates for the key.
type rfc2136 struct {
	server    string
	zone      string
	keyName   string
	algorithm string
	secret    []byte
	// now is the clock for the signature; tests set it.
	now func() time.Time
}

var tsigAlgorithms = map[string]func() hash.Hash{
	"hmac-sha1":   sha1.New,
	"hmac-sha224": sha256.New224,
	"hmac-sha256": sha256.New,
	"hmac-sha384": sha512.New384,
	"hmac-sha512": sha512.New,
}

// DNS constants used here.
const (
	dnsTypeA    = 1
	dnsTypeSOA  = 6
	dnsTypeTXT  = 16
	dnsTypeAAAA = 28
	dnsTypeTSIG = 250
	dnsClassIN  = 1
	dnsClassANY = 255
	dnsNone     = 254
	opUpdate    = 5
)

// rr is one resource record of the update section.
type rr struct {
	name  string
	kind  uint16
	class uint16
	ttl   uint32
	data  []byte
}

// appendName writes a name in wire format, without compression.
func appendName(b []byte, name string) []byte {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name != "" {
		for _, label := range strings.Split(name, ".") {
			b = append(b, byte(len(label)))
			b = append(b, label...)
		}
	}
	return append(b, 0)
}

func appendRR(b []byte, r rr) []byte {
	b = appendName(b, r.name)
	b = binary.BigEndian.AppendUint16(b, r.kind)
	b = binary.BigEndian.AppendUint16(b, r.class)
	b = binary.BigEndian.AppendUint32(b, r.ttl)
	b = binary.BigEndian.AppendUint16(b, uint16(len(r.data)))
	return append(b, r.data...)
}

func txtData(value string) []byte {
	var out []byte
	for len(value) > 255 {
		out = append(out, 255)
		out = append(out, value[:255]...)
		value = value[255:]
	}
	out = append(out, byte(len(value)))
	return append(out, value...)
}

// message builds an update of the zone with the given records.
func (r *rfc2136) message(id uint16, updates []rr) []byte {
	b := binary.BigEndian.AppendUint16(nil, id)
	b = binary.BigEndian.AppendUint16(b, opUpdate<<11)
	b = binary.BigEndian.AppendUint16(b, 1)                    // zone
	b = binary.BigEndian.AppendUint16(b, 0)                    // prerequisites
	b = binary.BigEndian.AppendUint16(b, uint16(len(updates))) // updates
	b = binary.BigEndian.AppendUint16(b, 0)                    // additional, TSIG comes later
	b = appendName(b, r.zone)
	b = binary.BigEndian.AppendUint16(b, dnsTypeSOA)
	b = binary.BigEndian.AppendUint16(b, dnsClassIN)
	for _, u := range updates {
		b = appendRR(b, u)
	}
	return b
}

// sign appends the TSIG record to a message (RFC 8945, section 4.3).
func sign(msg []byte, keyName, algorithm string, secret []byte, now time.Time) ([]byte, error) {
	newHash, ok := tsigAlgorithms[algorithm]
	if !ok {
		return nil, invalid("algorithm")
	}
	signed := uint64(now.Unix())
	const fudge = 300
	timers := func(b []byte) []byte {
		b = append(b, byte(signed>>40), byte(signed>>32), byte(signed>>24), byte(signed>>16), byte(signed>>8), byte(signed))
		return binary.BigEndian.AppendUint16(b, fudge)
	}
	variables := appendName(nil, keyName)
	variables = binary.BigEndian.AppendUint16(variables, dnsClassANY)
	variables = binary.BigEndian.AppendUint32(variables, 0)
	variables = appendName(variables, algorithm)
	variables = timers(variables)
	variables = binary.BigEndian.AppendUint16(variables, 0) // error
	variables = binary.BigEndian.AppendUint16(variables, 0) // other length
	mac := hmac.New(newHash, secret)
	mac.Write(msg)
	mac.Write(variables)
	sum := mac.Sum(nil)

	data := appendName(nil, algorithm)
	data = timers(data)
	data = binary.BigEndian.AppendUint16(data, uint16(len(sum)))
	data = append(data, sum...)
	data = append(data, msg[0], msg[1]) // original ID
	data = binary.BigEndian.AppendUint16(data, 0)
	data = binary.BigEndian.AppendUint16(data, 0)
	out := append([]byte(nil), msg...)
	out = appendRR(out, rr{name: keyName, kind: dnsTypeTSIG, class: dnsClassANY, ttl: 0, data: data})
	binary.BigEndian.PutUint16(out[10:], binary.BigEndian.Uint16(out[10:])+1)
	return out, nil
}

var rcodes = map[int]string{
	1: "FORMERR", 2: "SERVFAIL", 3: "NXDOMAIN", 4: "NOTIMP", 5: "REFUSED", 6: "YXDOMAIN",
	7: "YXRRSET", 8: "NXRRSET", 9: "NOTAUTH (key or zone not accepted)", 10: "NOTZONE (name not in the zone)",
	16: "BADSIG", 17: "BADKEY", 18: "BADTIME (clock of the NAS?)",
}

func (r *rfc2136) send(ctx context.Context, updates []rr) error {
	var idBytes [2]byte
	rand.Read(idBytes[:])
	id := binary.BigEndian.Uint16(idBytes[:])
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	msg, err := sign(r.message(id, updates), r.keyName, r.algorithm, r.secret, now())
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", r.server)
	if err != nil {
		return fmt.Errorf("RFC 2136: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(msg)))
	if _, err := conn.Write(append(frame, msg...)); err != nil {
		return fmt.Errorf("RFC 2136: %w", err)
	}
	var length [2]byte
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return fmt.Errorf("RFC 2136: no answer: %w", err)
	}
	answer := make([]byte, binary.BigEndian.Uint16(length[:]))
	if _, err := io.ReadFull(conn, answer); err != nil || len(answer) < 12 {
		return errors.New("RFC 2136: incomplete answer")
	}
	if binary.BigEndian.Uint16(answer) != id {
		return errors.New("RFC 2136: answer to another request")
	}
	if code := int(answer[3] & 0x0f); code != 0 {
		text := rcodes[code]
		if text == "" {
			text = fmt.Sprint(code)
		}
		return &APIError{Provider: "RFC 2136", Message: "the name server refused the update: " + text}
	}
	return nil
}

func (r *rfc2136) checkZone(fqdn string) error {
	if !inZone(fqdn, r.zone) {
		return &APIError{Provider: "RFC 2136", Message: fqdn + " is not in the zone " + r.zone}
	}
	return nil
}

// deleteRRset removes all records of a type at a name.
func deleteRRset(name string, kind uint16) rr {
	return rr{name: name, kind: kind, class: dnsClassANY}
}

func (r *rfc2136) SetTXT(ctx context.Context, fqdn string, values []string) error {
	if err := r.checkZone(fqdn); err != nil {
		return err
	}
	updates := []rr{deleteRRset(fqdn, dnsTypeTXT)}
	for _, value := range values {
		updates = append(updates, rr{name: fqdn, kind: dnsTypeTXT, class: dnsClassIN, ttl: 60, data: txtData(value)})
	}
	return r.send(ctx, updates)
}

func (r *rfc2136) ClearTXT(ctx context.Context, fqdn string) error {
	if err := r.checkZone(fqdn); err != nil {
		return err
	}
	return r.send(ctx, []rr{deleteRRset(fqdn, dnsTypeTXT)})
}

func (r *rfc2136) MultipleTXT() bool { return true }

func (r *rfc2136) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	if err := r.checkZone(host); err != nil {
		return err
	}
	var updates []rr
	if v4.IsValid() {
		ip := v4.Unmap().As4()
		updates = append(updates, deleteRRset(host, dnsTypeA), rr{name: host, kind: dnsTypeA, class: dnsClassIN, ttl: 60, data: ip[:]})
	}
	if v6.IsValid() {
		ip := v6.As16()
		updates = append(updates, deleteRRset(host, dnsTypeAAAA), rr{name: host, kind: dnsTypeAAAA, class: dnsClassIN, ttl: 60, data: ip[:]})
	}
	if len(updates) == 0 {
		return nil
	}
	return r.send(ctx, updates)
}
