package homeassistant

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// mqttKeepAlive is announced to the broker. TitanOS pings every half of it
// and gives up on a connection that answers nothing for 1.5 times it, or
// leaves a ping unanswered until the next one is due: a half-open TCP
// connection is otherwise only noticed after many minutes, while the broker
// has long published the last will.
var mqttKeepAlive = 60 * time.Second

// A small MQTT 3.1.1 client with what TitanOS needs: connect with a last
// will, publish with QoS 0 (retained where Home Assistant needs it) and
// subscribe to command topics. Only the standard library, like the rest
// of titand's protocol code.

const (
	mqttConnect     = 1
	mqttConnack     = 2
	mqttPublish     = 3
	mqttSubscribe   = 8
	mqttSuback      = 9
	mqttPingreq     = 12
	mqttPingresp    = 13
	mqttDisconnect  = 14
	mqttMaxIncoming = 64 << 10
)

// MQTTOptions say where the broker is and how to log in.
type MQTTOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	TLS      bool
	ClientID string
	// Will is published by the broker when the connection is lost.
	WillTopic, WillPayload string
}

type mqttClient struct {
	conn net.Conn
	r    *bufio.Reader

	wmu sync.Mutex
	// incoming gets PUBLISH packets for subscriptions.
	incoming  chan mqttMessage
	errs      chan error
	packetID  uint16
	ignored   sync.Map // own live discovery echoes never occupy the input queue
	done      chan struct{}
	closeOnce sync.Once
	// pingSent is the time of the unanswered PINGREQ (UnixNano), 0 if none.
	pingSent  atomic.Int64
	keepAlive time.Duration
}

type mqttMessage struct {
	Topic   string
	Payload []byte
}

func mqttString(s string) []byte {
	b := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	copy(b[2:], s)
	return b
}

func mqttLength(n int) []byte {
	var b []byte
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 0x80
		}
		b = append(b, digit)
		if n == 0 {
			return b
		}
	}
}

func mqttPacket(header byte, body []byte) []byte {
	return append(append([]byte{header}, mqttLength(len(body))...), body...)
}

// dialMQTT connects and logs in; the returned client reads in the background.
func dialMQTT(ctx context.Context, o MQTTOptions) (*mqttClient, error) {
	address := net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if o.TLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: o.Host}}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	c := &mqttClient{conn: conn, r: bufio.NewReader(conn), incoming: make(chan mqttMessage, 32), errs: make(chan error, 1), done: make(chan struct{}), keepAlive: mqttKeepAlive}

	flags := byte(0x02) // clean session
	var payload []byte
	payload = append(payload, mqttString(o.ClientID)...)
	if o.WillTopic != "" {
		flags |= 0x04 | 0x08 | 0x20 // will, QoS 1, retain
		payload = append(payload, mqttString(o.WillTopic)...)
		payload = append(payload, mqttString(o.WillPayload)...)
	}
	if o.Username != "" {
		flags |= 0x80
		payload = append(payload, mqttString(o.Username)...)
		if o.Password != "" {
			flags |= 0x40
			payload = append(payload, mqttString(o.Password)...)
		}
	}
	keepAlive := max(1, int(c.keepAlive/time.Second))
	body := append(mqttString("MQTT"), 4, flags, byte(keepAlive>>8), byte(keepAlive)) // protocol level 4
	body = append(body, payload...)
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write(mqttPacket(mqttConnect<<4, body)); err != nil {
		conn.Close()
		return nil, err
	}
	kind, answer, err := c.readPacket()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if kind>>4 != mqttConnack || len(answer) < 2 {
		conn.Close()
		return nil, errors.New("the broker did not accept the connection")
	}
	if code := answer[1]; code != 0 {
		conn.Close()
		reasons := map[byte]string{1: "protocol version", 2: "client ID", 3: "server unavailable", 4: "wrong user name or password", 5: "not authorized"}
		return nil, fmt.Errorf("the broker refused the connection: %s", reasons[code])
	}
	conn.SetDeadline(time.Time{})
	go c.readLoop()
	return c, nil
}

func (c *mqttClient) readPacket() (byte, []byte, error) {
	header, err := c.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, multiplier := 0, 1
	for i := 0; ; i++ {
		digit, err := c.r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		length += int(digit&0x7f) * multiplier
		if digit&0x80 == 0 {
			break
		}
		multiplier *= 128
		if i == 3 {
			return 0, nil, errors.New("malformed packet length")
		}
	}
	if length > mqttMaxIncoming {
		return 0, nil, errors.New("packet too large")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return 0, nil, err
	}
	return header, body, nil
}

func (c *mqttClient) readLoop() {
	for {
		// Anything from the broker counts as a sign of life; PINGRESP is
		// due well within this, so silence means the connection is dead.
		c.conn.SetReadDeadline(time.Now().Add(c.keepAlive * 3 / 2))
		header, body, err := c.readPacket()
		if err != nil {
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				err = errors.New("the broker stopped answering")
			}
			// No DISCONNECT: the broker is to publish the last will.
			c.conn.Close()
			c.errs <- err
			close(c.incoming)
			return
		}
		switch header >> 4 {
		case mqttPublish:
			if len(body) < 2 {
				continue
			}
			n := int(binary.BigEndian.Uint16(body))
			if len(body) < 2+n {
				continue
			}
			topic, rest := string(body[2:2+n]), body[2+n:]
			if qos := header >> 1 & 3; qos > 0 {
				if len(rest) < 2 {
					continue
				}
				rest = rest[2:] // packet ID; subscriptions are QoS 0, so no answer is owed
			}
			if strings.HasSuffix(topic, "/config") {
				if len(rest) == 0 {
					continue
				}
				if _, own := c.ignored.Load(topic); own {
					continue
				}
				// Retained discovery must not be lost in a burst. Ordinary
				// commands retain their bounded, non-blocking queue below.
				select {
				case c.incoming <- mqttMessage{topic, rest}:
				case <-c.done:
					return
				}
				continue
			}
			select {
			case c.incoming <- mqttMessage{topic, rest}:
			default: // a flood of commands is dropped, not queued
			}
		case mqttPingresp:
			c.pingSent.Store(0)
		case mqttSuback:
		}
	}
}

func (c *mqttClient) write(packet []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := c.conn.Write(packet)
	return err
}

// Publish sends a message with QoS 0.
func (c *mqttClient) Publish(topic string, payload []byte, retain bool) error {
	header := byte(mqttPublish << 4)
	if retain {
		header |= 1
	}
	return c.write(mqttPacket(header, append(mqttString(topic), payload...)))
}

// Subscribe asks for messages on topic filters with QoS 0.
func (c *mqttClient) Subscribe(filters ...string) error {
	if len(filters) == 0 {
		return nil
	}
	c.packetID++
	body := []byte{byte(c.packetID >> 8), byte(c.packetID)}
	for _, f := range filters {
		body = append(append(body, mqttString(f)...), 0)
	}
	return c.write(mqttPacket(mqttSubscribe<<4|0x02, body))
}

// Ping sends a PINGREQ; it fails when the previous one is still unanswered.
func (c *mqttClient) Ping() error {
	if sent := c.pingSent.Load(); sent != 0 && time.Since(time.Unix(0, sent)) >= c.keepAlive/4 {
		c.conn.Close() // no DISCONNECT: the broker is to publish the last will
		return errors.New("the broker did not answer the keep-alive ping")
	}
	c.pingSent.CompareAndSwap(0, time.Now().UnixNano())
	return c.write([]byte{mqttPingreq << 4, 0})
}

// Close says goodbye; the broker then does not send the last will.
func (c *mqttClient) Close() {
	c.closeOnce.Do(func() { close(c.done); _ = c.write([]byte{mqttDisconnect << 4, 0}); c.conn.Close() }) // cleanup, best effort: nothing more can be done if it fails
}
