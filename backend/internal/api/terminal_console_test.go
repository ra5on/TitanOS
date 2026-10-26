package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The VM screen takes the same rules as the serial console: terminals
// switched on and the password entered again, before any WebSocket opens.
func TestVMScreenFollowsTheConsoleRules(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	address := consoleServer(t, ts, vmScreenConsole("test"))
	plain := "http" + strings.TrimPrefix(address, "ws")

	get := func() (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", plain, nil)
		base, _ := url.Parse(ts.url)
		for _, cookie := range ts.client.Jar.Cookies(base) {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res, body
	}

	// No password again: a plain request gets past the checks and is only
	// refused by the WebSocket upgrade.
	res, body := get()
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("screen with the login alone: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/terminal", map[string]bool{"enabled": false}); res.StatusCode >= 300 {
		t.Fatalf("switch off: %d %s", res.StatusCode, body)
	}
	res, body = get()
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "terminal_off" {
		t.Errorf("screen with terminals off: %d %s", res.StatusCode, body)
	}

	// Switched on again, the login is enough.
	ts.request("PUT", "/api/v1/terminal", map[string]bool{"enabled": true})
	dialConsole(t, ts, ts.client, address)

	// The route itself still says first when the VM does not run.
	res, body = ts.request("GET", "/api/v1/vms/test/screen", nil)
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "vm_not_running" {
		t.Errorf("screen of a VM that does not run: %d %s", res.StatusCode, body)
	}
}

func TestVNCInput(t *testing.T) {
	update := []byte{3, 1, 0, 0, 0, 0, 4, 0, 3, 0}
	for _, tc := range []struct {
		data  []byte
		input bool
	}{
		{update, false},
		{append(append([]byte{}, update...), update...), false},
		{[]byte{4, 1, 0, 0, 0, 0, 0, 0x61}, true},         // key
		{[]byte{5, 0, 0, 10, 0, 20}, true},                // pointer
		{append(append([]byte{}, update...), 5, 0), true}, // update and more
		{[]byte("RFB 003.008\n"), true},                   // handshake
		{[]byte{3, 7, 0, 0, 0, 0, 4, 0, 3, 0}, true},      // not an update
		{nil, true},
	} {
		if got := vncInput(tc.data); got != tc.input {
			t.Errorf("vncInput(%v) = %v, want %v", tc.data, got, tc.input)
		}
	}
}

// consoleServer serves a console that echoes behind the checks of the real
// routes, and returns its WebSocket address.
func consoleServer(t *testing.T, ts *testServer, opts consoleOptions) string {
	t.Helper()
	srv := httptest.NewServer(ts.server.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		ts.server.serveStream(w, r, opts, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
			ours, theirs := net.Pipe()
			go func() {
				defer theirs.Close()
				io.Copy(theirs, theirs)
			}()
			return ours, nil, nil
		})
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// openConsole is a console as the browser sees it. closed is closed when
// the server ends it; everything the console sends is read and dropped.
type openConsole struct {
	conn   *websocket.Conn
	closed chan struct{}
}

// dialConsole opens the console with the cookies of a client.
func dialConsole(t *testing.T, ts *testServer, client *http.Client, address string) openConsole {
	t.Helper()
	base, _ := url.Parse(ts.url)
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(base) {
		header.Add("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	// The console answers: it is open. (A context ending during Read
	// closes the connection, so reads below never have a deadline.)
	if err := conn.Write(context.Background(), websocket.MessageBinary, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := conn.Read(context.Background()); err != nil || string(data) != "hi" {
		t.Fatalf("echo: %q %v", data, err)
	}
	console := openConsole{conn: conn, closed: make(chan struct{})}
	go func() {
		defer close(console.closed)
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	return console
}

// ended waits for the server to end a console.
func ended(console openConsole, within time.Duration) bool {
	select {
	case <-console.closed:
		return true
	case <-time.After(within):
		return false
	}
}

func TestConsoleEndsWithItsLogin(t *testing.T) {
	recheck := consoleRecheck.Swap(int64(20 * time.Millisecond))
	t.Cleanup(func() { consoleRecheck.Store(recheck) })

	ts := newTestServer(t)
	ts.setup() // owner "alex"
	address := consoleServer(t, ts, consoleOptions{target: "test"})

	login := func(name string) *http.Client {
		client := ts.newClient()
		if res, body := ts.do(client, "POST", "/api/v1/login", credentials{Name: name, Password: testPassword}); res.StatusCode != http.StatusOK {
			t.Fatalf("login %s: %d %s", name, res.StatusCode, body)
		}
		return client
	}
	admin := func(name string) *http.Client {
		if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": name, "password": testPassword, "role": auth.RoleAdmin}); res.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, res.StatusCode, body)
		}
		return login(name)
	}

	for _, tc := range []struct {
		name string
		end  func(name string, client *http.Client)
	}{
		{"logout", func(_ string, client *http.Client) {
			ts.do(client, "POST", "/api/v1/logout", nil)
		}},
		{"device ended", func(name string, client *http.Client) {
			_, body := ts.do(client, "GET", "/api/v1/account/sessions", nil)
			id := strings.SplitN(strings.SplitN(string(body), `"id":"`, 2)[1], `"`, 2)[0]
			// From another device of the same account.
			other := login(name)
			if res, body := ts.do(other, "DELETE", "/api/v1/account/sessions/"+id, nil); res.StatusCode >= 300 {
				t.Fatalf("end session: %d %s", res.StatusCode, body)
			}
		}},
		{"demoted", func(name string, _ *http.Client) {
			if res, body := ts.request("PUT", "/api/v1/users/"+name, map[string]any{"role": auth.RoleUser}); res.StatusCode != http.StatusOK {
				t.Fatalf("demote: %d %s", res.StatusCode, body)
			}
		}},
		{"deleted", func(name string, _ *http.Client) {
			if res, body := ts.request("DELETE", "/api/v1/users/"+name, nil); res.StatusCode >= 300 {
				t.Fatalf("delete: %d %s", res.StatusCode, body)
			}
		}},
		{"password reset", func(name string, _ *http.Client) {
			if res, body := ts.request("POST", "/api/v1/users/"+name+"/password", map[string]any{"password": "another long password"}); res.StatusCode >= 300 {
				t.Fatalf("reset: %d %s", res.StatusCode, body)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "k" + strings.ReplaceAll(tc.name, " ", "")[:6]
			client := admin(name)
			conn := dialConsole(t, ts, client, address)
			// A console whose login holds stays open.
			if ended(conn, 150*time.Millisecond) {
				t.Fatal("the console ended on its own")
			}
			tc.end(name, client)
			if !ended(conn, 3*time.Second) {
				t.Error("the console stayed open")
			}
		})
	}

	// Switching terminals off still ends what is open.
	conn := dialConsole(t, ts, ts.client, address)
	ts.request("PUT", "/api/v1/terminal", map[string]bool{"enabled": false})
	if !ended(conn, 3*time.Second) {
		t.Error("the console stayed open with terminals off")
	}
}

// The screen of a VM ends after the idle time when nobody types or moves
// the mouse, even though the viewer keeps asking for pictures.
func TestScreenIdleCountsOnlyInput(t *testing.T) {
	idle := consoleIdle.Swap(int64(300 * time.Millisecond))
	t.Cleanup(func() { consoleIdle.Store(idle) })

	ts := newTestServer(t)
	ts.setup()
	address := consoleServer(t, ts, consoleOptions{target: "screen", raw: true, input: vncInput})
	conn := dialConsole(t, ts, ts.client, address)

	update := []byte{3, 1, 0, 0, 0, 0, 4, 0, 3, 0}
	key := []byte{4, 1, 0, 0, 0, 0, 0, 0x61}
	// Keys keep it open past the idle time.
	for range 8 {
		if conn.conn.Write(context.Background(), websocket.MessageBinary, key) != nil || ended(conn, 100*time.Millisecond) {
			t.Fatal("ended although someone typed")
		}
	}
	// Picture requests alone do not.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if conn.conn.Write(context.Background(), websocket.MessageBinary, update) != nil || ended(conn, 50*time.Millisecond) {
			return
		}
	}
	t.Error("picture requests kept the screen open")
}
