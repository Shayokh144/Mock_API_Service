// Package sessions runs the developer proxy and short-lived test proxies.
package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const (
	// DefaultID is the developer session. It cannot be deleted.
	DefaultID = "default"
	// MaxTestSessions is the cap on sessions other than default.
	MaxTestSessions = 10
	// DefaultIdle is how long a quiet test session stays open.
	DefaultIdle = 30 * time.Minute
)

// ErrDefault is returned when a caller tries to delete the developer session.
var ErrDefault = errors.New("the default session cannot be deleted")

// ErrNotFound means the session id is not open.
var ErrNotFound = errors.New("session not found")

// ErrPortBusy means the requested port is not free.
var ErrPortBusy = errors.New("port is in use")

// ErrTooMany means ten test sessions are already open.
var ErrTooMany = errors.New("too many test sessions")

// ErrBadRequest is a validation failure.
var ErrBadRequest = errors.New("invalid session")

// Info is the public description of one session.
type Info struct {
	ID          string
	Name        string
	Port        int
	ProxyURL    string
	EmulatorURL string
	ReadOnly    bool
	Rules       *rules.Store
	History     *history.Log
}

// Options configures the session manager.
type Options struct {
	Rules        *rules.Store
	History      *history.Log
	Upstreams    *upstreams.Store
	ProxyAddr    string
	AdminAddr    string
	Timeout      time.Duration
	HistoryLimit int
}

// Manager tracks the developer session and test sessions.
type Manager struct {
	mu        sync.Mutex
	opts      Options
	proxyPort int
	adminPort int
	tests     map[string]*live
	now       func() time.Time
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type live struct {
	info Info
	idle time.Duration
	last time.Time
	srv  *http.Server
	mu   sync.Mutex
}

// New starts the idle sweeper. The developer session is the existing proxy.
func New(opts Options) *Manager {
	m := &Manager{
		opts:      opts,
		proxyPort: portOf(opts.ProxyAddr, 8080),
		adminPort: portOf(opts.AdminAddr, 8081),
		tests:     map[string]*live{},
		now:       time.Now,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go m.loop()
	return m
}

// SetNow replaces the clock. Tests use it to expire idle sessions.
func (m *Manager) SetNow(now func() time.Time) {
	m.mu.Lock()
	m.now = now
	m.mu.Unlock()
}

// Close stops test listeners. The developer proxy is left to the process.
func (m *Manager) Close() {
	m.closeOnce.Do(func() { close(m.stop) })
	<-m.done
	m.mu.Lock()
	all := m.tests
	m.tests = map[string]*live{}
	m.mu.Unlock()
	for _, item := range all {
		item.close()
	}
}

// Upstreams is the shared upstream store.
func (m *Manager) Upstreams() *upstreams.Store {
	return m.opts.Upstreams
}

// List returns the developer session first, then test sessions.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Info{m.defaultInfo()}
	for _, item := range m.tests {
		out = append(out, item.snapshot())
	}
	return out
}

// Get returns one session. The developer session id is "default".
func (m *Manager) Get(id string) (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == DefaultID {
		return m.defaultInfo(), true
	}
	item, ok := m.tests[id]
	if !ok {
		return Info{}, false
	}
	return item.snapshot(), true
}

// Touch marks API or proxy activity so the idle timer resets.
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	item := m.tests[id]
	now := m.now()
	m.mu.Unlock()
	if item != nil {
		item.touch(now)
	}
}

// Create opens a loopback proxy for a test. port 0 picks a free port.
func (m *Manager) Create(name string, port int, idle time.Duration) (Info, error) {
	if port < 0 || port > 65535 {
		return Info{}, fmt.Errorf("%w: port is out of range", ErrBadRequest)
	}
	if name == "" {
		name = "session"
	}
	if idle <= 0 {
		idle = DefaultIdle
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tests) >= MaxTestSessions {
		return Info{}, ErrTooMany
	}
	if port != 0 && (port == m.proxyPort || port == m.adminPort || m.portUsed(port)) {
		return Info{}, ErrPortBusy
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return Info{}, ErrPortBusy
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return Info{}, ErrPortBusy
	}
	id, err := newID()
	if err != nil {
		_ = ln.Close()
		return Info{}, err
	}
	rulesStore := rules.NewMemory()
	log := history.New(m.opts.HistoryLimit)
	handler := proxy.New(proxy.Options{
		Upstreams: m.opts.Upstreams,
		Timeout:   m.opts.Timeout,
		Rules:     rulesStore,
		History:   log,
	})
	item := &live{
		info: Info{
			ID:          id,
			Name:        name,
			Port:        tcp.Port,
			ProxyURL:    "http://127.0.0.1:" + strconv.Itoa(tcp.Port),
			EmulatorURL: "http://10.0.2.2:" + strconv.Itoa(tcp.Port),
			ReadOnly:    true,
			Rules:       rulesStore,
			History:     log,
		},
		idle: idle,
		last: m.now(),
	}
	item.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			item.touch(m.current())
			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
	}
	m.tests[id] = item
	go func() { _ = item.srv.Serve(ln) }()
	return item.snapshot(), nil
}

// Delete closes a test session. The developer session cannot be deleted.
func (m *Manager) Delete(id string) error {
	if id == DefaultID {
		return ErrDefault
	}
	m.mu.Lock()
	item, ok := m.tests[id]
	if ok {
		delete(m.tests, id)
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	item.close()
	return nil
}

// Sweep closes test sessions that have been idle too long.
func (m *Manager) Sweep() {
	m.mu.Lock()
	now := m.now()
	var stale []*live
	for id, item := range m.tests {
		if item.idleFor(now) {
			delete(m.tests, id)
			stale = append(stale, item)
		}
	}
	m.mu.Unlock()
	for _, item := range stale {
		item.close()
	}
}

func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.Sweep()
		}
	}
}

func (m *Manager) current() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now()
}

func (m *Manager) defaultInfo() Info {
	return Info{
		ID:          DefaultID,
		Name:        DefaultID,
		Port:        m.proxyPort,
		ProxyURL:    "http://127.0.0.1:" + strconv.Itoa(m.proxyPort),
		EmulatorURL: "http://10.0.2.2:" + strconv.Itoa(m.proxyPort),
		Rules:       m.opts.Rules,
		History:     m.opts.History,
	}
}

func (m *Manager) portUsed(port int) bool {
	for _, item := range m.tests {
		if item.info.Port == port {
			return true
		}
	}
	return false
}

func (s *live) snapshot() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

func (s *live) touch(now time.Time) {
	s.mu.Lock()
	s.last = now
	s.mu.Unlock()
}

func (s *live) idleFor(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Sub(s.last) >= s.idle
}

func (s *live) close() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

func portOf(addr string, fallback int) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return fallback
	}
	return n
}

func newID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
