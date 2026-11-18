package davsync

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// memConfig is /config in RAM (devices.json, pim.json).
type memConfig struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (c *memConfig) Read(name string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (c *memConfig) Write(name string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[name] = append([]byte(nil), data...)
	return nil
}

type memUsers map[string]auth.User

func (u memUsers) Get(name string) (auth.User, bool) {
	user, ok := u[name]
	return user, ok
}

// env is a real davsync handler over a real store on a temporary volume
// and real device passwords, served over TLS.
type env struct {
	t       *testing.T
	store   *store.Store
	devices *devices.Manager
	limiter *auth.Limiter
	h       *Handler
	srv     *httptest.Server
	config  *memConfig
	users   memUsers

	mu sync.Mutex
	// volume is the current system data volume ("" = gone); dirs are the
	// folders of the volumes.
	volume string
	dirs   map[string]string
	// passwords: "alex" and "bea" are device passwords of the accounts,
	// "account" alex's account password (refused).
	passwords map[string]string
	// opens counts the opens of the volume (Volume hook): an unchanged
	// state is served from RAM (disk sleep, TECHNIK 5.4).
	opens atomic.Int32
	// requestOpens are the opens during the last replayed request.
	requestOpens int32
	// churned counts the events of @churn (new names each time).
	churned int
	// saved are the values a sequence keeps (< save …, {saved:NAME}).
	saved map[string]string
	// tasksGone: only the reminder lists cannot be read (@tasks off).
	tasksGone bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	created := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	e := &env{t: t, limiter: auth.NewLimiter(), config: &memConfig{files: map[string][]byte{}},
		users: memUsers{
			"alex": {Name: "alex", Role: auth.RoleOwner, Created: created},
			"bea":  {Name: "bea", Role: auth.RoleUser, Created: created.Add(time.Hour)},
		},
		volume: "disk1", dirs: map[string]string{"disk1": t.TempDir(), "disk2": t.TempDir()},
		passwords: map[string]string{"account": "the-account-password"}, saved: map[string]string{}}
	st := store.New()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	// The zone of the NAS: dates and floating times (never the machine's).
	st.Local = func() *time.Location { return berlin }
	st.Current = func(feature string) string {
		e.mu.Lock()
		defer e.mu.Unlock()
		if feature == string(store.Tasks) && e.tasksGone {
			return ""
		}
		return e.volume
	}
	st.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		e.mu.Lock()
		volume := e.volume
		if feature == string(store.Tasks) && e.tasksGone {
			volume = ""
		}
		dir := e.dirs[volume]
		e.mu.Unlock()
		e.opens.Add(1)
		if volume == "" {
			return nil, nil, volumedata.ErrUnavailable
		}
		path := filepath.Join(dir, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, nil, err
		}
		return root, func() {}, nil
	}
	st.Account = func(name string) (time.Time, bool) {
		u, ok := e.users[name]
		return u.Created, ok
	}
	st.ReadMark = func(account string) ([]byte, error) { return e.config.Read("users/" + account + "/pim.json") }
	st.WriteMark = func(account string, data []byte) error { return e.config.Write("users/"+account+"/pim.json", data) }
	e.store = st
	e.devices = devices.New(e.config, e.users, func() *auth.Limiter { return e.limiter }, st)
	for _, name := range []string{"alex", "bea"} {
		c, err := e.devices.Create(e.users[name], "iPhone")
		if err != nil {
			t.Fatal(err)
		}
		e.passwords[name] = c.Password
	}
	e.h = New(st, e.devices)
	e.srv = httptest.NewTLSServer(e.h)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) setVolume(v string) {
	e.mu.Lock()
	e.volume = v
	e.mu.Unlock()
	if v != "" {
		e.store.Moved(string(store.Calendar))
		e.store.Moved(string(store.Contacts))
		e.store.Moved(string(store.Tasks))
	}
}

// client follows no redirect by itself (iOS keeps PROPFIND on a 301; Go
// would turn it into GET).
func (e *env) client() *http.Client {
	c := e.srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func (e *env) put(kind store.Kind, account, col, name string, data []byte) {
	e.t.Helper()
	if _, err := e.store.Put(kind, account, col, name, data, store.PutOptions{}); err != nil {
		e.t.Fatalf("seed %s/%s: %v", col, name, err)
	}
}
