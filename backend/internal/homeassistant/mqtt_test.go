package homeassistant

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"slices"
	"strconv"
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
	// connects counts logins; the first deaf ones never get a PINGRESP,
	// like a half-open connection. online records which logins announced
	// availability.
	connects int
	deaf     int
	online   map[int]bool
}

func startBroker(t *testing.T) *fakeBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBroker{ln: ln, retained: map[string]string{}, states: map[string]string{}, online: map[int]bool{}}
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
	login := 0
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
			b.connects++
			login = b.connects
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
			if strings.HasPrefix(topic, "titanos/") && strings.HasSuffix(topic, "/status") && payload == "online" {
				b.online[login] = true
			}
		case mqttSubscribe:
			n := int(binary.BigEndian.Uint16(body[2:]))
			b.subs = append(b.subs, string(body[4:4+n]))
			conn.Write([]byte{mqttSuback << 4, 3, body[0], body[1], 0})
		case mqttPingreq:
			if login > b.deaf {
				conn.Write([]byte{mqttPingresp << 4, 0})
			}
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
	containers := []RunValues{{Name: "immich_server", Running: true}, {Name: "pihole", Running: false}}
	used := 42.0
	source := func(context.Context) Snapshot {
		mu.Lock()
		defer mu.Unlock()
		return Snapshot{Host: "nas", Version: "0.14.0", System: &SystemValues{CPUPercent: 12.3, MemoryPercent: 40},
			Volumes: []VolumeValues{{Name: "data", State: "ok", UsedPercent: &used}}, Containers: append([]RunValues{}, containers...), VMs: []RunValues{{Name: "win11", Running: true}}}
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
	eventually(t, "discovery", func() bool {
		return broker.get(func(b *fakeBroker) bool { return b.retained[cfg] != "" && len(b.subs) == 3 })
	})
	broker.get(func(b *fakeBroker) bool {
		// Login, last will (QoS 1, retained) and the command subscription.
		if !strings.Contains(string(b.connect), "titanos/abc123/status") || !strings.Contains(string(b.connect), "nas") || b.connect[7]&0x3c != 0x2c {
			t.Errorf("connect %q", b.connect)
		}
		if len(b.subs) != 3 || !slices.Contains(b.subs, "titanos/abc123/cmd/#") {
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
		var sw map[string]any
		json.Unmarshal([]byte(b.retained["homeassistant/switch/titanos_abc123/container_pihole/config"]), &sw)
		if _, invalid := sw["expire_after"]; invalid {
			t.Error("MQTT switches do not support sensor expiration")
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

// A connection whose broker stops answering pings is given up: the status
// says disconnected, TitanOS reconnects and announces itself again.
func TestMQTTDeadConnectionReconnects(t *testing.T) {
	broker := startBroker(t)
	broker.deaf = 1
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{MQTT: MQTT{Enabled: true, Host: "127.0.0.1", Port: broker.port(), Groups: []string{"system"}, ID: "abc123"}}); err != nil {
		t.Fatal(err)
	}
	source := func(context.Context) Snapshot {
		return Snapshot{Host: "nas", System: &SystemValues{CPUPercent: 1}}
	}
	oldKeep, oldRetry := mqttKeepAlive, mqttRetry
	mqttKeepAlive, mqttRetry = time.Second, 50*time.Millisecond
	defer func() { mqttKeepAlive, mqttRetry = oldKeep, oldRetry }()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	defer func() { cancel(); <-stopped }()
	go func() { m.RunMQTT(ctx, source, nil); close(stopped) }()

	eventually(t, "first login", func() bool { return broker.get(func(b *fakeBroker) bool { return b.online[1] }) })
	if connected, _ := m.MQTTStatus(); !connected {
		t.Fatal("not connected after login")
	}
	start := time.Now()
	sawDown := false
	eventually(t, "reconnect", func() bool {
		if connected, msg := m.MQTTStatus(); !connected && strings.Contains(msg, "answer") {
			sawDown = true
		}
		return broker.get(func(b *fakeBroker) bool { return b.connects >= 2 && b.online[2] })
	})
	if !sawDown {
		t.Error("status never said disconnected")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("dead connection noticed after %v", d)
	}
	if connected, _ := m.MQTTStatus(); !connected {
		t.Error("not connected after reconnect")
	}
	// The healthy second connection stays up.
	time.Sleep(2 * time.Second)
	if broker.get(func(b *fakeBroker) bool { return b.connects != 2 }) {
		t.Error("healthy connection dropped")
	}
}

// A flood of presses runs at most mqttCommandLimit commands at once and at
// most one per target; repeated presses are dropped while one is pending.
func TestMQTTCommandFloodBounded(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	running, peak, total := 0, 0, 0
	r := newCommandRunner(func(ctx context.Context, kind, name, action string) error {
		mu.Lock()
		running++
		total++
		peak = max(peak, running)
		mu.Unlock()
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := 0
	for i := 0; i < 5000; i++ {
		if r.submit(ctx, "container", "c"+strconv.Itoa(i%5), "restart") {
			accepted++
		}
	}
	if accepted != 5 {
		t.Errorf("accepted %d commands for 5 targets", accepted)
	}
	eventually(t, "running", func() bool { mu.Lock(); defer mu.Unlock(); return running == mqttCommandLimit })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if peak > mqttCommandLimit {
		t.Errorf("%d commands at once", peak)
	}
	mu.Unlock()
	close(release)
	eventually(t, "drained", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.pending) == 0
	})
	mu.Lock()
	defer mu.Unlock()
	if total != 5 || peak > mqttCommandLimit {
		t.Errorf("total %d, peak %d", total, peak)
	}
	// Once done, the same target can be pressed again.
	if !r.submit(ctx, "container", "c0", "restart") {
		t.Error("target still blocked after completion")
	}
}
