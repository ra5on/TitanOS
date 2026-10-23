package homeassistant

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeBroker accepts one client and records what it publishes.
type fakeBroker struct {
	ln       net.Listener
	mu       sync.Mutex
	connect  []byte
	retained map[string]string
	states   map[string]string
	subs     []string
	conn     net.Conn
}

func startBroker(t *testing.T) *fakeBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBroker{ln: ln, retained: map[string]string{}, states: map[string]string{}}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	return b
}

func (b *fakeBroker) port() int { return b.ln.Addr().(*net.TCPAddr).Port }

func (b *fakeBroker) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	c := &mqttClient{r: r}
	for {
		header, body, err := c.readPacket()
		if err != nil {
			return
		}
		b.mu.Lock()
		switch header >> 4 {
		case mqttConnect:
			b.connect = body
			b.conn = conn
			conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		case mqttPublish:
			n := int(binary.BigEndian.Uint16(body))
			topic, payload := string(body[2:2+n]), string(body[2+n:])
			if header&1 == 1 {
				b.retained[topic] = payload
			}
			if strings.Contains(topic, "/state/") || strings.HasSuffix(topic, "/status") {
				b.states[topic] = payload
			}
		case mqttSubscribe:
			n := int(binary.BigEndian.Uint16(body[2:]))
			b.subs = append(b.subs, string(body[4:4+n]))
			conn.Write([]byte{mqttSuback << 4, 3, body[0], body[1], 0})
		case mqttPingreq:
			conn.Write([]byte{mqttPingresp << 4, 0})
		case mqttDisconnect:
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}

// send publishes a message to the client, as a press in Home Assistant does.
func (b *fakeBroker) send(topic, payload string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conn.Write(mqttPacket(mqttPublish<<4, append(mqttString(topic), payload...)))
}

func (b *fakeBroker) get(f func(b *fakeBroker) bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return f(b)
}

func TestMQTTDiscoveryStatesAndCommands(t *testing.T) {
	broker := startBroker(t)
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{Account: "alex", MQTT: MQTT{Enabled: true, Host: "127.0.0.1", Port: broker.port(), Username: "nas", Password: "pw",
		Groups: []string{"system", "volumes", "containers"}, Commands: []string{"containers"}, ID: "abc123"}}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	containers := []RunValues{{"immich_server", true}, {"pihole", false}}
	used := 42.0
	source := func(context.Context) Snapshot {
		mu.Lock()
		defer mu.Unlock()
		return Snapshot{Host: "nas", Version: "0.14.0", System: &SystemValues{CPUPercent: 12.3, MemoryPercent: 40},
			Volumes: []VolumeValues{{Name: "data", State: "ok", UsedPercent: &used}}, Containers: append([]RunValues{}, containers...), VMs: []RunValues{{"win11", true}}}
	}
	var cmds []string
	commands := func(_ context.Context, kind, name, action string) error {
		mu.Lock()
		cmds = append(cmds, kind+"/"+name+"/"+action)
		mu.Unlock()
		return nil
	}
	old := mqttInterval
	mqttInterval = 50 * time.Millisecond
	defer func() { mqttInterval = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.RunMQTT(ctx, source, commands)

	cfg := "homeassistant/sensor/titanos_abc123/cpu/config"
	eventually(t, "discovery", func() bool { return broker.get(func(b *fakeBroker) bool { return b.retained[cfg] != "" }) })
	broker.get(func(b *fakeBroker) bool {
		// Login, last will (QoS 1, retained) and the command subscription.
		if !strings.Contains(string(b.connect), "titanos/abc123/status") || !strings.Contains(string(b.connect), "nas") || b.connect[7]&0x3c != 0x2c {
			t.Errorf("connect %q", b.connect)
		}
		if len(b.subs) != 1 || b.subs[0] != "titanos/abc123/cmd/#" {
			t.Errorf("subs %v", b.subs)
		}
		var c map[string]any
		json.Unmarshal([]byte(b.retained[cfg]), &c)
		if c["state_topic"] != "titanos/abc123/state/cpu" || c["unique_id"] != "titanos_abc123_cpu" || c["availability_topic"] != "titanos/abc123/status" {
			t.Errorf("config %v", c)
		}
		if b.states["titanos/abc123/state/cpu"] != "12.3" || b.states["titanos/abc123/state/volume_data_used"] != "42.0" || b.states["titanos/abc123/status"] != "online" {
			t.Errorf("states %v", b.states)
		}
		// Containers with commands are switches; VMs are neither sensors nor switches here.
		if b.retained["homeassistant/switch/titanos_abc123/container_pihole/config"] == "" || b.states["titanos/abc123/state/container_immich_server"] != "ON" {
			t.Errorf("switch missing: %v", b.retained)
		}
		for topic := range b.retained {
			if strings.Contains(topic, "vm_win11") {
				t.Errorf("vm published: %s", topic)
			}
		}
		return true
	})

	// A press in Home Assistant reaches the commander; a command of a group
	// that is off is ignored.
	broker.send("titanos/abc123/cmd/container/pihole", "start")
	broker.send("titanos/abc123/cmd/vm/win11", "start")
	broker.send("titanos/abc123/cmd/container/pihole", "rm -rf")
	eventually(t, "command", func() bool { mu.Lock(); defer mu.Unlock(); return len(cmds) == 1 })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if strings.Join(cmds, ",") != "container/pihole/start" {
		t.Errorf("commands %v", cmds)
	}
	// A removed container disappears from Home Assistant.
	containers = containers[:1]
	mu.Unlock()
	gone := "homeassistant/switch/titanos_abc123/container_pihole/config"
	eventually(t, "removal", func() bool { return broker.get(func(b *fakeBroker) bool { return b.retained[gone] == "" }) })
}

func TestMQTTRefusedLogin(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.ReadAtLeast(conn, make([]byte, 10), 10)
		conn.Write([]byte{mqttConnack << 4, 2, 0, 4})
	}()
	_, err = dialMQTT(context.Background(), MQTTOptions{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, ClientID: "x"})
	if err == nil || !strings.Contains(err.Error(), "wrong user name or password") {
		t.Fatalf("refused login: %v", err)
	}
}

func TestMQTTSettings(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	for _, q := range []MQTT{
		{Enabled: true, Host: "broker; rm"},
		{Host: "b", Groups: []string{"everything"}},
		{Host: "b", Commands: []string{"shell"}},
		{Host: "b", Prefix: "../x"},
		{Host: "b", Port: 70000},
	} {
		if err := m.SetSettings(Settings{MQTT: q}); err == nil {
			t.Errorf("%+v accepted", q)
		}
	}
	if err := m.SetSettings(Settings{MQTT: MQTT{Host: "b", TLS: true, Password: "secret"}}); err != nil {
		t.Fatal(err)
	}
	s, set := m.Settings()
	if s.MQTT.Port != 8883 || s.MQTT.Prefix != "homeassistant" || s.MQTT.Password != "" || !set["mqtt"] {
		t.Fatalf("%+v %v", s.MQTT, set)
	}
	if !allowedCommand(MQTT{Commands: []string{"power"}}, "system", "power", "poweroff") || allowedCommand(MQTT{Commands: []string{"updates"}}, "system", "power", "reboot") {
		t.Fatal("power commands")
	}
}
